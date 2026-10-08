#include "trainer.h"

#include <BLEDevice.h>

namespace {

const BLEUUID kService("6e40fec1-b5a3-f393-e0a9-e50e24dcca9e");
const BLEUUID kNotify("6e40fec2-b5a3-f393-e0a9-e50e24dcca9e");
const BLEUUID kWrite("6e40fec3-b5a3-f393-e0a9-e50e24dcca9e");

constexpr uint32_t kScanSeconds = 5;

Trainer* self = nullptr;
BLEClient* client = nullptr;
BLEAdvertisedDevice* found = nullptr;
BLERemoteCharacteristic* writeChr = nullptr;
bool writeWithResponse = false;
portMUX_TYPE mux = portMUX_INITIALIZER_UNLOCKED;

class ScanCallbacks : public BLEAdvertisedDeviceCallbacks {
  void onResult(BLEAdvertisedDevice dev) override { self->onFound(&dev); }
};

class ClientCallbacks : public BLEClientCallbacks {
  void onConnect(BLEClient*) override {}
  void onDisconnect(BLEClient*) override { self->onDisconnected(); }
};

ScanCallbacks scanCallbacks;
ClientCallbacks clientCallbacks;

void scanDone(BLEScanResults) { self->onScanDone(); }

void notify(BLERemoteCharacteristic*, uint8_t* data, size_t len, bool) { self->onData(data, len); }

}  // namespace

void Trainer::begin(float riderKg, float bikeKg, float wheelM) {
  self = this;
  riderKg_ = riderKg;
  bikeKg_ = bikeKg;
  wheelM_ = wheelM;
  BLEDevice::init("caycle");
  BLEScan* scan = BLEDevice::getScan();
  scan->setAdvertisedDeviceCallbacks(&scanCallbacks);
  scan->setActiveScan(true);  // Tacx puts its name in the scan response
  scan->setInterval(100);
  scan->setWindow(99);
  client = BLEDevice::createClient();
  client->setClientCallbacks(&clientCallbacks);
  startScan();
}

const char* Trainer::status() const {
  switch (state_) {
    case State::Connected: return "connected";
    case State::Found:
    case State::Connecting: return "connecting";
    default: return "searching";
  }
}

void Trainer::startScan() {
  state_ = State::Scanning;
  BLEScan* scan = BLEDevice::getScan();
  scan->clearResults();
  scan->start(kScanSeconds, scanDone, false);
}

void Trainer::onFound(void* advertisedDevice) {
  BLEAdvertisedDevice& dev = *static_cast<BLEAdvertisedDevice*>(advertisedDevice);
  if (state_ != State::Scanning) return;
  const bool tacx = dev.haveName() && dev.getName().indexOf("Tacx") >= 0;
  const bool fecService = dev.haveServiceUUID() && dev.isAdvertisingService(kService);
  if (!tacx && !fecService) return;
  delete found;
  found = new BLEAdvertisedDevice(dev);
  name_ = dev.haveName() ? dev.getName() : String("Tacx");
  state_ = State::Found;
  BLEDevice::getScan()->stop();
}

void Trainer::onScanDone() {
  if (state_ == State::Scanning) state_ = State::Idle;  // nothing found; loop() rescans
}

void Trainer::onDisconnected() {
  if (state_ == State::Connected) {
    Serial.println("trainer disconnected");
  }
  writeChr = nullptr;
  state_ = State::Idle;
  retryAt_ = millis() + 1000;
}

void Trainer::onData(const uint8_t* data, size_t len) {
  // A notification may carry several 13-byte ANT frames.
  fec::Page page;
  while (len >= fec::kFrameLen) {
    if (!fec::decode(data, len, page)) {
      data++;
      len--;
      continue;
    }
    portENTER_CRITICAL(&mux);
    fec::apply(page, metrics_);
    updatedAt_ = millis();
    portEXIT_CRITICAL(&mux);
    data += fec::kFrameLen;
    len -= fec::kFrameLen;
  }
}

fec::Metrics Trainer::metrics(uint32_t* ageMs) const {
  portENTER_CRITICAL(&mux);
  fec::Metrics m = metrics_;
  const uint32_t at = updatedAt_;
  portEXIT_CRITICAL(&mux);
  if (ageMs) *ageMs = at ? millis() - at : UINT32_MAX;
  return m;
}

bool Trainer::connect() {
  state_ = State::Connecting;
  Serial.printf("connecting to %s\n", name_.c_str());
  if (!client->connect(found)) {
    Serial.println("connect failed");
    return false;
  }
  BLERemoteService* svc = client->getService(kService);
  BLERemoteCharacteristic* notifyChr = svc ? svc->getCharacteristic(kNotify) : nullptr;
  BLERemoteCharacteristic* w = svc ? svc->getCharacteristic(kWrite) : nullptr;
  if (!notifyChr || !w || !notifyChr->canNotify()) {
    Serial.println("trainer has no FE-C over BLE service");
    client->disconnect();
    return false;
  }
  writeWithResponse = w->canWrite();
  writeChr = w;
  notifyChr->registerForNotify(notify);

  portENTER_CRITICAL(&mux);
  metrics_ = fec::Metrics();
  updatedAt_ = 0;
  portEXIT_CRITICAL(&mux);
  state_ = State::Connected;
  epoch_++;

  fec::Page cfg;
  fec::userConfig(riderKg_, bikeKg_, wheelM_, cfg);
  send(cfg);
  Serial.printf("connected to %s\n", name_.c_str());
  return true;
}

void Trainer::loop() {
  switch (state_) {
    case State::Found:
      if (!connect()) {
        state_ = State::Idle;
        retryAt_ = millis() + 2000;
      }
      break;
    case State::Idle:
      if (static_cast<int32_t>(millis() - retryAt_) >= 0) startScan();
      break;
    default:
      break;
  }
}

bool Trainer::send(const fec::Page page) {
  if (state_ != State::Connected || !writeChr) return false;
  uint8_t frame[fec::kFrameLen];
  fec::encode(page, frame);
  return writeChr->writeValue(frame, sizeof(frame), writeWithResponse);
}

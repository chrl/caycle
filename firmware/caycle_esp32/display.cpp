#include "display.h"

#include <Adafruit_GFX.h>
#include <Adafruit_SSD1306.h>
#include <Fonts/FreeSansBold18pt7b.h>
#include <Wire.h>

namespace {

// Heltec WiFi LoRa 32 (V3) wiring.
constexpr int kVextPin = 36;  // powers the OLED, active LOW
constexpr int kSdaPin = 17;
constexpr int kSclPin = 18;
constexpr int kResetPin = 21;
constexpr int kWidth = 128;
constexpr int kHeight = 64;
constexpr int kSideX = 90;  // right column

Adafruit_SSD1306 oled(kWidth, kHeight, &Wire, kResetPin);

void textRight(const char* s, int y, int size = 1) {
  oled.setTextSize(size);
  const int w = static_cast<int>(strlen(s)) * 6 * size;
  oled.setCursor(kWidth - w, y);
  oled.print(s);
}

}  // namespace

namespace display {

bool begin() {
  pinMode(kVextPin, OUTPUT);
  digitalWrite(kVextPin, LOW);
  delay(50);
  Wire.begin(kSdaPin, kSclPin);
  if (!oled.begin(SSD1306_SWITCHCAPVCC, 0x3C)) return false;
  oled.setTextColor(SSD1306_WHITE);
  oled.cp437(true);
  return true;
}

void splash(const char* line, const char* corner) {
  oled.clearDisplay();
  oled.setFont(nullptr);
  oled.setTextSize(2);
  oled.setCursor(22, 8);
  oled.print("caycle");
  oled.setTextSize(1);
  oled.setCursor(0, 40);
  oled.print(line);
  textRight(corner, 56);
  oled.display();
}

void draw(const Screen& s) {
  oled.clearDisplay();
  oled.setFont(nullptr);
  oled.setTextSize(1);

  if (!s.connected) {
    splash(s.status, s.battery);
    return;
  }

  // Top line: phase on the left, countdown on the right.
  oled.setCursor(0, 0);
  oled.print(s.header);
  textRight(s.right, 0);
  if (s.progress >= 0) {
    oled.drawRect(0, 10, kWidth, 4, SSD1306_WHITE);
    oled.fillRect(0, 10, static_cast<int>(kWidth * min(1.0f, s.progress)), 4, SSD1306_WHITE);
  }

  // Big power number.
  char buf[16];
  if (s.power >= 0) {
    snprintf(buf, sizeof(buf), "%d", s.power);
  } else {
    strcpy(buf, "--");
  }
  oled.setFont(&FreeSansBold18pt7b);
  oled.setCursor(0, 46);
  oled.print(buf);
  oled.setFont(nullptr);
  oled.setTextSize(1);
  oled.setCursor(oled.getCursorX() + 2, 39);  // classic font draws from the top-left
  oled.print("W");

  // Right column: target or setting.
  oled.setCursor(kSideX, 20);
  oled.print(s.sideLabel);
  oled.setTextSize(2);
  oled.setCursor(kSideX, 32);
  oled.print(s.side);
  oled.setTextSize(1);

  // Bottom line: cadence and speed, or a blinking warning.
  const bool blinkOn = (millis() / 500) % 2 == 0;
  if (s.warning && blinkOn) {
    oled.fillRect(0, 54, kWidth, 10, SSD1306_WHITE);
    oled.setTextColor(SSD1306_BLACK);
    const int w = static_cast<int>(strlen(s.warning)) * 6;
    oled.setCursor((kWidth - w) / 2, 55);
    oled.print(s.warning);
    oled.setTextColor(SSD1306_WHITE);
  } else if (s.note.length()) {
    const int w = static_cast<int>(s.note.length()) * 6;
    oled.setCursor((kWidth - w) / 2, 56);
    oled.print(s.note);
  } else {
    // Cadence left, speed centred, distance right: "92rpm 31.4km/h 12.3km".
    if (s.cadence >= 0) {
      snprintf(buf, sizeof(buf), "%drpm", s.cadence);
    } else {
      strcpy(buf, "--rpm");
    }
    oled.setCursor(0, 56);
    oled.print(buf);
    if (s.distanceKm >= 0) {
      snprintf(buf, sizeof(buf), s.distanceKm < 100 ? "%.1fkm" : "%.0fkm", s.distanceKm);
      textRight(buf, 56);
      snprintf(buf, sizeof(buf), "%.1fkm/h", s.speedKmh);
      oled.setCursor((kWidth - static_cast<int>(strlen(buf)) * 6) / 2, 56);
      oled.print(buf);
    } else {
      snprintf(buf, sizeof(buf), "%.1f km/h", s.speedKmh);
      textRight(buf, 56);
    }
  }
  oled.display();
}

}  // namespace display

// LiPo voltage on the Heltec WiFi LoRa 32 (V3): GPIO1 reads the battery through
// a 390k/100k divider that GPIO37 switches on.
#pragma once

namespace battery {

void begin();
void loop();       // re-measures every few seconds
float volts();     // smoothed voltage, 0 if no battery reading
int percent();     // rough LiPo charge estimate, -1 if unknown

}  // namespace battery

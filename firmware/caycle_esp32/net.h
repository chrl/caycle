// Wi-Fi, clock sync and Strava uploads, run in a background task so the
// display and the trainer link never wait on the network.
#pragma once

#include <Arduino.h>

namespace net {

void begin();          // loads settings; syncs the clock and uploads pending rides
void requestSync();    // upload pending rides now
void requestCheck();   // test the Strava login (refreshes the token)

// Short status for the display, e.g. "ON STRAVA", "NO WIFI". Empty when idle.
String status();
bool busy();

void setWifiSsid(const String& ssid);
void setWifiPassword(const String& password);
void setStrava(const String& clientId, const String& clientSecret, const String& refreshToken);
String describe();  // configuration summary for the serial console

}  // namespace net

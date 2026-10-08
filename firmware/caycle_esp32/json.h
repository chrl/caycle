// Minimal JSON field lookup for flat Strava responses (no nesting needed).
#pragma once

#include <stddef.h>
#include <stdint.h>

namespace json {

// Copies the value of the first "key" in a JSON object into out: strings are
// unescaped, numbers/booleans copied as text. Returns false if the key is
// missing or null.
bool get(const char* doc, const char* key, char* out, size_t n);

// Integer value of "key" (also accepts numeric strings), false if missing/null.
bool getInt(const char* doc, const char* key, int64_t* out);

}  // namespace json

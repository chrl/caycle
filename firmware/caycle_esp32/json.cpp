#include "json.h"

#include <stdlib.h>
#include <string.h>

namespace json {
namespace {

const char* skipSpace(const char* p) {
  while (*p == ' ' || *p == '\t' || *p == '\n' || *p == '\r') p++;
  return p;
}

// Finds the value that follows "key": at any nesting level.
const char* find(const char* doc, const char* key) {
  const size_t klen = strlen(key);
  for (const char* p = doc; (p = strchr(p, '"')) != nullptr; p++) {
    if (strncmp(p + 1, key, klen) == 0 && p[klen + 1] == '"') {
      const char* q = skipSpace(p + klen + 2);
      if (*q == ':') return skipSpace(q + 1);
    }
    // Skip the rest of this string so keys inside values don't match.
    for (p++; *p && *p != '"'; p++) {
      if (*p == '\\' && p[1]) p++;
    }
    if (!*p) return nullptr;
  }
  return nullptr;
}

}  // namespace

bool get(const char* doc, const char* key, char* out, size_t n) {
  if (n == 0) return false;
  const char* v = find(doc, key);
  if (!v || strncmp(v, "null", 4) == 0) return false;
  size_t i = 0;
  if (*v == '"') {
    for (v++; *v && *v != '"'; v++) {
      char c = *v;
      if (c == '\\' && v[1]) {
        v++;
        switch (*v) {
          case 'n': c = '\n'; break;
          case 't': c = '\t'; break;
          case 'r': c = '\r'; break;
          case 'u': c = '?'; v += (strlen(v) >= 5) ? 4 : 0; break;  // non-ASCII is not needed here
          default: c = *v; break;
        }
      }
      if (i + 1 < n) out[i++] = c;
    }
  } else {
    for (; *v && *v != ',' && *v != '}' && *v != ']' && *v != ' ' && *v != '\n'; v++) {
      if (i + 1 < n) out[i++] = *v;
    }
  }
  out[i] = '\0';
  return true;
}

bool getInt(const char* doc, const char* key, int64_t* out) {
  char buf[24];
  if (!get(doc, key, buf, sizeof(buf)) || !buf[0]) return false;
  char* end;
  const long long v = strtoll(buf, &end, 10);
  if (end == buf) return false;
  *out = v;
  return true;
}

}  // namespace json

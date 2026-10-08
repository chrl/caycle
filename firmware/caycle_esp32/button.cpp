#include "button.h"

Press Button::update(bool pressed, uint32_t now) {
  if (pressed != last_) {
    last_ = pressed;
    changedAt_ = now;
  }
  if (now - changedAt_ >= kDebounceMs && pressed != stable_) {
    stable_ = pressed;
    if (stable_) {  // press started
      downAt_ = now;
      longFired_ = false;
    } else if (!longFired_) {  // released before it became a long press
      if (pendingShort_ && now - upAt_ <= kDoubleGapMs + kLongMs) {
        pendingShort_ = false;
        return Press::Double;
      }
      pendingShort_ = true;
      upAt_ = now;
    }
  }
  if (stable_ && !longFired_ && now - downAt_ >= kLongMs) {
    longFired_ = true;
    pendingShort_ = false;
    return Press::Long;
  }
  // A short press is only reported once no second press followed it.
  if (pendingShort_ && !stable_ && !last_ && now - upAt_ > kDoubleGapMs) {
    pendingShort_ = false;
    return Press::Short;
  }
  return Press::None;
}

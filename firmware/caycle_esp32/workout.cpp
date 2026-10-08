#include "workout.h"

namespace workout {

const char* kindName(Kind k) {
  switch (k) {
    case Kind::Warmup: return "WARMUP";
    case Kind::Work: return "WORK";
    case Kind::Recover: return "REST";
    case Kind::Cooldown: return "COOLDOWN";
  }
  return "";
}

uint32_t Rng::next() {
  s_ ^= s_ << 13;
  s_ ^= s_ >> 17;
  s_ ^= s_ << 5;
  return s_;
}

int Rng::range(int lo, int hi) { return lo + static_cast<int>(next() % static_cast<uint32_t>(hi - lo + 1)); }

uint32_t Plan::totalSeconds() const {
  uint32_t s = 0;
  for (int i = 0; i < count; i++) s += steps[i].seconds;
  return s;
}

namespace {

struct Effort {
  uint16_t seconds;
  uint8_t minPct, maxPct;
};

// Shorter efforts are harder.
constexpr Effort kEfforts[] = {
    {30, 130, 150}, {45, 125, 140}, {60, 120, 135}, {90, 112, 125},
    {120, 108, 120}, {180, 103, 113}, {240, 100, 110},
};

constexpr uint16_t kWarmupStep = 120;  // 3 x 2 min ramp
constexpr uint16_t kCooldown = 300;
constexpr int kMinMinutes = 20;

int roundTo(int v, int step) { return (v + step / 2) / step * step; }

void add(Plan& p, Kind kind, uint16_t seconds, uint16_t pct) {
  if (p.count < kMaxSteps) p.steps[p.count++] = {kind, seconds, pct};
}

}  // namespace

Plan generate(uint32_t seed, int minutes) {
  if (minutes < kMinMinutes) minutes = kMinMinutes;
  Rng rng(seed);
  Plan p;
  add(p, Kind::Warmup, kWarmupStep, 50);
  add(p, Kind::Warmup, kWarmupStep, 60);
  add(p, Kind::Warmup, kWarmupStep, 70);

  const int mainSeconds = minutes * 60 - 3 * kWarmupStep - kCooldown;
  int used = 0;
  // Keep room for the cool-down step and the last recovery.
  while (p.count < kMaxSteps - 3) {
    const Effort& e = kEfforts[rng.range(0, sizeof(kEfforts) / sizeof(kEfforts[0]) - 1)];
    const int rest = roundTo(e.seconds * rng.range(75, 150) / 100, 15);
    const int recover = rest < 60 ? 60 : (rest > 240 ? 240 : rest);
    if (used + e.seconds + recover > mainSeconds) break;
    add(p, Kind::Work, e.seconds, static_cast<uint16_t>(roundTo(rng.range(e.minPct, e.maxPct), 5)));
    p.workCount++;
    add(p, Kind::Recover, static_cast<uint16_t>(recover), static_cast<uint16_t>(rng.range(10, 12) * 5));
    used += e.seconds + recover;
  }
  // Spread leftover time over the recoveries so the plan hits the requested length.
  const int recoveries = p.workCount;
  if (recoveries > 0) {
    const int leftover = mainSeconds - used;
    const int extra = leftover / recoveries / 5 * 5;  // keep durations on 5 s steps
    for (int i = 3; i < p.count; i++) {
      if (p.steps[i].kind == Kind::Recover) p.steps[i].seconds += static_cast<uint16_t>(extra);
    }
    p.steps[p.count - 1].seconds += static_cast<uint16_t>(leftover - extra * recoveries);
  } else {
    add(p, Kind::Recover, static_cast<uint16_t>(mainSeconds), 55);
  }
  add(p, Kind::Cooldown, kCooldown, 50);
  return p;
}

void Player::load(const Plan& plan) {
  plan_ = plan;
  stop();
}

void Player::start() {
  index_ = 0;
  stepMs_ = 0;
  elapsedMs_ = 0;
  state_ = plan_.count > 0 ? State::Running : State::Idle;
  changed_ = true;
}

void Player::togglePause() {
  if (state_ == State::Running) {
    state_ = State::Paused;
  } else if (state_ == State::Paused) {
    state_ = State::Running;
  } else {
    return;
  }
  changed_ = true;
}

void Player::skip() {
  if (state_ == State::Running || state_ == State::Paused) advance();
}

void Player::stop() {
  state_ = State::Idle;
  index_ = 0;
  stepMs_ = 0;
  elapsedMs_ = 0;
  changed_ = true;
}

void Player::advance() {
  stepMs_ = 0;
  changed_ = true;
  if (index_ + 1 >= plan_.count) {
    state_ = State::Done;
    return;
  }
  index_++;
}

void Player::tick(uint32_t ms, bool pedaling) {
  if (state_ != State::Running || !pedaling) return;
  elapsedMs_ += ms;
  stepMs_ += ms;
  while (state_ == State::Running && stepMs_ >= step().seconds * 1000u) {
    const uint32_t over = stepMs_ - step().seconds * 1000u;
    advance();
    stepMs_ = state_ == State::Running ? over : 0;
  }
}

uint32_t Player::stepRemainingMs() const {
  const uint32_t total = step().seconds * 1000u;
  return stepMs_ >= total ? 0 : total - stepMs_;
}

float Player::stepProgress() const {
  return step().seconds ? static_cast<float>(stepMs_) / (step().seconds * 1000.0f) : 0;
}

int Player::workNumber() const {
  int n = 0;
  for (int i = 0; i <= index_ && i < plan_.count; i++) {
    if (plan_.steps[i].kind == Kind::Work) n++;
  }
  return n;
}

bool Player::changed() {
  const bool c = changed_;
  changed_ = false;
  return c;
}

int Player::targetWatts(int ftp, int biasPct) const {
  const Step& s = step();
  // The bias applies to work blocks only; recovery stays easy.
  const int pct = s.kind == Kind::Work ? s.pct + biasPct : s.pct;
  return (ftp * pct + 50) / 100;
}

}  // namespace workout

// Random ERG interval workouts. Pure logic (no Arduino), unit-tested on the host.
#pragma once

#include <stdint.h>

namespace workout {

enum class Kind : uint8_t { Warmup, Work, Recover, Cooldown };

const char* kindName(Kind k);

struct Step {
  Kind kind;
  uint16_t seconds;
  uint16_t pct;  // % of FTP
};

constexpr int kMaxSteps = 64;

// Small deterministic PRNG (xorshift32) so workouts are reproducible in tests.
class Rng {
 public:
  explicit Rng(uint32_t seed) : s_(seed ? seed : 0x9E3779B9u) {}
  uint32_t next();
  int range(int lo, int hi);  // inclusive

 private:
  uint32_t s_;
};

// A plan: warm-up ramp, random work/recovery blocks, cool-down.
struct Plan {
  Step steps[kMaxSteps];
  int count = 0;
  int workCount = 0;
  uint32_t totalSeconds() const;
};

// Builds a plan of about `minutes` (at least 20). Shorter efforts are harder:
// 30 s at 130-150 % FTP down to 4 min at 100-110 %.
Plan generate(uint32_t seed, int minutes);

enum class State : uint8_t { Idle, Running, Paused, Done };

// Plays a plan. Time only advances while running and pedaling, so stopping
// to drink doesn't eat into an interval.
class Player {
 public:
  void load(const Plan& plan);
  void start();          // Idle/Done -> Running (from the first step)
  void togglePause();    // Running <-> Paused
  void skip();           // jump to the next step
  void stop();           // -> Idle
  void tick(uint32_t ms, bool pedaling);

  State state() const { return state_; }
  const Plan& plan() const { return plan_; }
  int index() const { return index_; }
  const Step& step() const { return plan_.steps[index_]; }
  uint32_t stepRemainingMs() const;
  uint32_t elapsedMs() const { return elapsedMs_; }
  float stepProgress() const;  // 0..1
  int workNumber() const;      // 1-based number of the current/last work block
  bool changed();              // true once after the step or state changed

  // Target in watts for the current step, given FTP and a difficulty bias in %.
  int targetWatts(int ftp, int biasPct) const;

 private:
  void advance();

  Plan plan_;
  State state_ = State::Idle;
  int index_ = 0;
  uint32_t stepMs_ = 0;
  uint32_t elapsedMs_ = 0;
  bool changed_ = false;
};

}  // namespace workout

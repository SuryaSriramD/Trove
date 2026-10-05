#pragma once
#include <atomic>
#include <chrono>
#include <memory>
#include <thread>

// The OS completion block owns this shared state too. A timed-out/cancelled
// caller never leaves a callback referring to its expired stack variables.
struct CaptureConsent { std::atomic<int> answer{0}; }; // pending, granted, denied
enum class CaptureConsentResult { granted, denied, cancelled, timed_out };
inline CaptureConsentResult wait_capture_consent(const std::shared_ptr<CaptureConsent>& state,
 const std::atomic<bool>& cancelled,std::chrono::steady_clock::time_point deadline) {
 for(;;) {
  if(cancelled.load())return CaptureConsentResult::cancelled;
  int answer=state->answer.load();
  if(answer)return answer==1?CaptureConsentResult::granted:CaptureConsentResult::denied;
  if(std::chrono::steady_clock::now()>=deadline)return CaptureConsentResult::timed_out;
  std::this_thread::sleep_for(std::chrono::milliseconds(5));
 }
}

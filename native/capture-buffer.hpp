#pragma once
#include <array>
#include <atomic>
#include <cmath>
#include <cstdint>
#include <algorithm>

// One producer (the miniaudio callback), one consumer (the durable writer).
// No allocations, locks, file I/O or device lifecycle calls in push().
struct CaptureBuffer {
 static constexpr uint64_t capacity=32768;
 std::array<float,capacity> samples{};
 std::atomic<uint64_t> written{0},read{0};
 std::atomic<int> failure{0}; // 1 overflow, 2 invalid sample, 3 device interrupted
 std::atomic<bool> accepting{true},signal{false};
 uint64_t maximum=0;
 static_assert(std::atomic<uint64_t>::is_always_lock_free,"capture requires lock-free counters");
 void push(const float *input,uint64_t count) {
  if(!accepting.load(std::memory_order_acquire))return;
  uint64_t w=written.load(std::memory_order_relaxed),r=read.load(std::memory_order_acquire);
  if(!input||count>capacity||w<r||w-r>capacity){failure.store(1);accepting.store(false);return;}
  count=std::min(count,maximum-w);
  if(count>capacity-(w-r)){failure.store(1);accepting.store(false);return;}
  for(uint64_t i=0;i<count;i++)if(!std::isfinite(input[i])||std::fabs(input[i])>1.01f){failure.store(2);accepting.store(false);return;}
  for(uint64_t i=0;i<count;i++){samples[(w+i)%capacity]=input[i];if(std::fabs(input[i])>0.00001f)signal.store(true,std::memory_order_relaxed);}
  written.store(w+count,std::memory_order_release);
  if(w+count>=maximum)accepting.store(false,std::memory_order_release);
 }
 uint64_t pop(float *output,uint64_t count) {
  uint64_t r=read.load(std::memory_order_relaxed),w=written.load(std::memory_order_acquire);
  count=std::min(count,w-r);
  for(uint64_t i=0;i<count;i++)output[i]=samples[(r+i)%capacity];
  read.store(r+count,std::memory_order_release);
  return count;
 }
};

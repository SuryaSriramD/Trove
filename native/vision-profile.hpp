#pragma once
#include "json.hpp"
#include "vision-profile-generated.hpp"
#include <stdexcept>
#include <string>

// Validate before model loading. The request cannot supply arbitrary geometry.
inline bool vision_profile_requested(const nlohmann::json &input, const nlohmann::json &wire) {
 const bool exact_pair = wire.at("inputs").at(0).at("sha256") == VISION_PROFILE_MODEL && wire.at("inputs").at(3).at("sha256") == VISION_PROFILE_PROJECTOR;
 const auto id = input.value("preprocessingProfileId", std::string{});
 const auto hash = input.value("preprocessingProfileHash", std::string{});
 if (exact_pair) {
  if (input.at("schemaVersion") != 2 || id != VISION_PROFILE_ID || hash != VISION_PROFILE_HASH) throw std::runtime_error("image preprocessing profile mismatch");
  return true;
 }
 if (input.at("schemaVersion") != 1 || !id.empty() || !hash.empty()) throw std::runtime_error("image preprocessing profile does not match model pair");
 return false;
}

import Foundation

struct ReminderRule: Codable {
    var id, revision, pageId, title, startDate, localTime, timezone, frequency: String
    var enabled: Bool
    var privateText: Bool
    enum CodingKeys: String, CodingKey { case id, revision, pageId, title, startDate, localTime, timezone, frequency, enabled; case privateText = "private" }
}
struct ReminderDelivery: Codable {
    var id, reminderId, revision, dueAt, outcome: String
    var action, snoozedUntil, error: String?
    var actionRevision: Int64?
}
struct ReminderSchedule: Codable {
    var version: Int
    var epoch: String
    var generation: Int64
    var workspaceId: String
    var active: Bool
    var rules: [ReminderRule]
    var overrides: [ReminderDelivery]
    var authorizationRequest: String?
}
struct ReminderReceipt: Codable {
    var version = 1
    var epoch = ""
    var generation: Int64 = 0
    var permission = "notDetermined"
    var acknowledgedAt = ""
    var deliveries: [ReminderDelivery] = []
    var error: String?
}
let reminderISO = ISO8601DateFormatter()
func reminderDate(_ value: String) -> Date? { reminderISO.date(from: value) }
func reminderString(_ value: Date) -> String { reminderISO.string(from: value) }

func reminderRuleValid(_ rule: ReminderRule) -> Bool {
    guard UUID(uuidString: rule.id) != nil, UUID(uuidString: rule.pageId) != nil, let revision = UInt64(rule.revision), revision > 0,
          rule.title.utf8.count <= 512, rule.timezone != "Local", TimeZone(identifier: rule.timezone) != nil,
          ["once", "daily", "weekly"].contains(rule.frequency), rule.startDate.range(of: "^[0-9]{4}-[0-9]{2}-[0-9]{2}$", options: .regularExpression) != nil,
          rule.localTime.range(of: "^([01][0-9]|2[0-3]):[0-5][0-9]$", options: .regularExpression) != nil else { return false }
    let parser = DateFormatter(); parser.locale = Locale(identifier: "en_US_POSIX"); parser.timeZone = TimeZone(secondsFromGMT: 0); parser.calendar = Calendar(identifier: .gregorian); parser.dateFormat = "yyyy-MM-dd"; parser.isLenient = false
    guard let date = parser.date(from: rule.startDate), parser.string(from: date) == rule.startDate, let year = Int(rule.startDate.prefix(4)), year >= 2000, year <= 2200 else { return false }
    return true
}
func reminderDeliveryValid(_ delivery: ReminderDelivery) -> Bool {
    guard UUID(uuidString: delivery.reminderId) != nil, let revision = UInt64(delivery.revision), revision > 0,
          let due = reminderDate(delivery.dueAt), delivery.id == delivery.reminderId + ":" + String(Int64(due.timeIntervalSince1970)),
          ["scheduled", "submitted", "failed", "unknown"].contains(delivery.outcome), [nil, "snoozed", "dismissed"].contains(delivery.action),
          (delivery.actionRevision ?? 0) >= 0, (delivery.actionRevision ?? 0) <= 9007199254740991 else { return false }
    if delivery.action == "snoozed" { return delivery.snoozedUntil.flatMap(reminderDate) != nil }
    return delivery.snoozedUntil == nil
}

// Same policy as internal/reminders: explicit timezone, earlier repeated time,
// first valid minute following a DST gap, no occurrence on a skipped date.
func reminderInstant(_ date: String, _ clock: String, _ timezone: String) -> Date? {
    guard let zone = TimeZone(identifier: timezone) else { return nil }
    let parts = (date + "-" + clock.replacingOccurrences(of: ":", with: "-")).split(separator: "-").compactMap { Int($0) }
    guard parts.count == 5 else { return nil }
    var calendar = Calendar(identifier: .gregorian); calendar.timeZone = zone
    guard let midday = calendar.date(from: DateComponents(year: parts[0], month: parts[1], day: parts[2], hour: 12)) else { return nil }
    let start = calendar.startOfDay(for: midday)
    guard let result = calendar.nextDate(after: start.addingTimeInterval(-1), matching: DateComponents(hour: parts[3], minute: parts[4], second: 0), matchingPolicy: .nextTime, repeatedTimePolicy: .first, direction: .forward) else { return nil }
    let actual = calendar.dateComponents([.year, .month, .day], from: result)
    return actual.year == parts[0] && actual.month == parts[1] && actual.day == parts[2] ? result : nil
}
func reminderOccurrences(_ rule: ReminderRule, after: Date?, through: Date, limit: Int = 1000) -> ([ReminderDelivery], Bool) {
    guard rule.enabled, let zone = TimeZone(identifier: rule.timezone), ["once", "daily", "weekly"].contains(rule.frequency) else { return ([], false) }
    var floating = Calendar(identifier: .gregorian); floating.timeZone = TimeZone(secondsFromGMT: 0)!
    let parser = DateFormatter(); parser.locale = Locale(identifier: "en_US_POSIX"); parser.calendar = floating; parser.timeZone = floating.timeZone; parser.dateFormat = "yyyy-MM-dd"; parser.isLenient = false
    let local = DateFormatter(); local.locale = parser.locale; local.calendar = floating; local.timeZone = zone; local.dateFormat = "yyyy-MM-dd"
    guard let first = parser.date(from: rule.startDate), let last = parser.date(from: local.string(from: through)) else { return ([], false) }
    var date = first
    if rule.frequency != "once", let after = after, let localAfter = parser.date(from: local.string(from: after)), let previous = floating.date(byAdding: .day, value: -1, to: localAfter), previous > date { date = previous }
    var out: [ReminderDelivery] = []
    let startDay = floating.component(.weekday, from: first)
    while date <= last {
        if rule.frequency != "weekly" || floating.component(.weekday, from: date) == startDay,
           let due = reminderInstant(parser.string(from: date), rule.localTime, rule.timezone), due <= through, after == nil || due > after! {
            if out.count == limit { return (out, true) }
            out.append(ReminderDelivery(id: rule.id + ":" + String(Int64(due.timeIntervalSince1970)), reminderId: rule.id, revision: rule.revision, dueAt: reminderString(due), outcome: "scheduled"))
        }
        if rule.frequency == "once" { break }
        guard let next = floating.date(byAdding: .day, value: 1, to: date), next > date else { break }; date = next
    }
    return (out, false)
}

import AppKit
import Foundation
import UserNotifications
import Darwin

final class ReminderAgentState: Codable {
    var receipt = ReminderReceipt()
    var cursors: [String: String] = [:]
    var notificationIDs: [String: String] = [:]
    var snoozesSent: [String: Bool] = [:]
    var lastTick: String?
    var episode: String?
    var summaryOutcome: String?
    var authorizationRequest: String?
}

final class ReminderAgent: NSObject, UNUserNotificationCenterDelegate {
    let root: URL
    lazy var center = UNUserNotificationCenter.current()
    var state = ReminderAgentState()
    var schedule: ReminderSchedule?
    var busy = false
    var pendingSubmissions = 0
    var lockFD: Int32 = -1
    var testOutcome: String?
    var testSubmissions: [String] = []
    init(root: URL, testOutcome: String? = nil) throws {
        self.root = root
        self.testOutcome = testOutcome
        super.init()
        try FileManager.default.createDirectory(at: root, withIntermediateDirectories: true, attributes: [.posixPermissions: 0o700])
        var metadata = stat(); guard lstat(root.path, &metadata) == 0, metadata.st_mode & S_IFMT == S_IFDIR, metadata.st_uid == getuid(), metadata.st_mode & 0o077 == 0 else { throw CocoaError(.fileReadNoPermission) }
        lockFD = open(root.appendingPathComponent("helper.lock").path, O_RDWR | O_CREAT | O_NOFOLLOW, 0o600)
        guard lockFD >= 0, flock(lockFD, LOCK_EX | LOCK_NB) == 0 else { throw CocoaError(.fileLocking) }
        if testOutcome != nil { return }
        center.delegate = self
        let snooze = UNNotificationAction(identifier: "snooze", title: "Snooze 15 minutes", options: [])
        let dismiss = UNNotificationAction(identifier: "dismiss", title: "Dismiss", options: [])
        center.setNotificationCategories([UNNotificationCategory(identifier: "reminder", actions: [snooze, dismiss], intentIdentifiers: [], options: [.customDismissAction])])
    }
    func read<T: Decodable>(_ name: String, as: T.Type) throws -> T {
        let url = root.appendingPathComponent(name)
        let fd = open(url.path, O_RDONLY | O_NOFOLLOW)
        guard fd >= 0 else { throw CocoaError(.fileReadNoPermission) }
        let handle = FileHandle(fileDescriptor: fd, closeOnDealloc: true)
        var metadata = stat()
        guard fstat(fd, &metadata) == 0, metadata.st_mode & S_IFMT == S_IFREG, metadata.st_uid == getuid(), metadata.st_mode & 0o077 == 0, metadata.st_size <= 64 << 20 else { throw CocoaError(.fileReadCorruptFile) }
        let data = try handle.read(upToCount: (64 << 20) + 1) ?? Data()
        guard data.count <= 64 << 20 else { throw CocoaError(.fileReadCorruptFile) }
        return try JSONDecoder().decode(T.self, from: data)
    }
    deinit { if lockFD >= 0 { close(lockFD) } }

    func write<T: Encodable>(_ value: T, _ name: String) throws {
        let data = try JSONEncoder().encode(value)
        guard data.count <= 64 << 20 else { throw CocoaError(.fileWriteOutOfSpace) }
        let tmp = root.appendingPathComponent(".stage-" + UUID().uuidString)
        let fd = open(tmp.path, O_CREAT | O_EXCL | O_WRONLY | O_NOFOLLOW, 0o600)
        guard fd >= 0 else { throw CocoaError(.fileWriteUnknown) }
        var success = false
        defer { close(fd); if !success { unlink(tmp.path) } }
        try data.withUnsafeBytes { raw in
            var count = 0
            while count < raw.count { let n = Darwin.write(fd, raw.baseAddress!.advanced(by: count), raw.count - count); guard n > 0 else { throw CocoaError(.fileWriteUnknown) }; count += n }
        }
        guard fsync(fd) == 0, rename(tmp.path, root.appendingPathComponent(name).path) == 0 else { throw CocoaError(.fileWriteUnknown) }
        let parent = open(root.path, O_RDONLY); if parent >= 0 { _ = fsync(parent); close(parent) }
        success = true
    }
    func save(_ target: ReminderAgentState) throws {
        try write(target, "state-" + target.receipt.epoch + ".json")
        try write(target.receipt, "receipt-" + target.receipt.epoch + ".json")
    }
    func permission(_ settings: UNNotificationSettings) -> String {
        switch settings.authorizationStatus { case .authorized: return "authorized"; case .provisional: return "provisional"; case .denied: return "denied"; case .notDetermined: return "notDetermined"; default: return "unknown" }
    }
    func tick() {
        if busy || pendingSubmissions > 0 { return }; busy = true
        do {
            let next = try read("schedule.json", as: ReminderSchedule.self)
            guard next.version == 1, UUID(uuidString: next.epoch) != nil, UUID(uuidString: next.workspaceId) != nil, next.generation > 0, next.generation <= 9007199254740991, next.rules.count <= 1000, next.overrides.count <= 100000,
                  Set(next.rules.map { $0.id }).count == next.rules.count, next.rules.allSatisfy(reminderRuleValid),
                  Set(next.overrides.map { $0.id }).count == next.overrides.count, next.overrides.allSatisfy(reminderDeliveryValid) else { throw CocoaError(.fileReadCorruptFile) }
            if schedule?.epoch != next.epoch {
                center.removeAllPendingNotificationRequests(); center.removeAllDeliveredNotifications()
                let name = "state-" + next.epoch + ".json"
                state = FileManager.default.fileExists(atPath: root.appendingPathComponent(name).path) ? try read(name, as: ReminderAgentState.self) : ReminderAgentState()
                state.receipt.epoch = next.epoch
            }
            guard next.generation >= state.receipt.generation else { throw CocoaError(.fileReadCorruptFile) }
            if next.generation != state.receipt.generation {
                center.removeAllPendingNotificationRequests()
                if !next.active { center.removeAllDeliveredNotifications() }
                state.receipt.generation = next.generation
                let keys = Set(next.rules.map { $0.id + ":" + $0.revision }); state.cursors = state.cursors.filter { keys.contains($0.key) }
                guard Set(state.receipt.deliveries.map { $0.id }).count == state.receipt.deliveries.count else { throw CocoaError(.fileReadCorruptFile) }
                var positions = Dictionary(uniqueKeysWithValues: state.receipt.deliveries.enumerated().map { ($0.element.id, $0.offset) })
                for override in next.overrides {
                    if let i = positions[override.id] {
                        if (override.actionRevision ?? 0) >= (state.receipt.deliveries[i].actionRevision ?? 0), let action = override.action, !action.isEmpty {
                            state.receipt.deliveries[i].actionRevision = override.actionRevision
                            state.receipt.deliveries[i].action = action
                            state.receipt.deliveries[i].snoozedUntil = override.snoozedUntil
                        }
                        if state.receipt.deliveries[i].outcome == "scheduled" { state.receipt.deliveries[i].outcome = override.outcome }
                        if override.action == "dismissed", let notificationID = state.notificationIDs[override.id] { center.removeDeliveredNotifications(withIdentifiers: [notificationID]) }
                    } else { positions[override.id] = state.receipt.deliveries.count; state.receipt.deliveries.append(override) }
                }
            }
            schedule = next
            if next.active, let request = next.authorizationRequest, request != state.authorizationRequest {
                state.authorizationRequest = request; try save(state)
                center.requestAuthorization(options: [.alert, .sound]) { _, _ in DispatchQueue.main.async { self.inspectPermission() } }
            } else { inspectPermission() }
        } catch {
            if !state.receipt.epoch.isEmpty { state.receipt.error = "Reminder publication could not be read or persisted. Reopen Trove to reconcile it."; try? save(state) }
            busy = false
        }
    }
    func inspectPermission() {
        center.getNotificationSettings { settings in DispatchQueue.main.async {
            self.state.receipt.permission = self.permission(settings)
            self.state.receipt.acknowledgedAt = reminderString(Date())
            do { try self.save(self.state); try self.process() } catch { self.state.receipt.error = "Reminder state could not be saved; delivery is paused."; try? self.save(self.state) }
            self.busy = false
        } }
    }
    func process(at now: Date = Date()) throws {
        guard let schedule = schedule, schedule.active else { return }
        let target = state
        if target.lastTick.flatMap(reminderDate).map({ now.timeIntervalSince($0) > 30 }) ?? true { if target.episode == nil { target.episode = UUID().uuidString.lowercased(); target.summaryOutcome = nil } }
        var more = false
        var known = Set(target.receipt.deliveries.map { $0.id })
        var remaining = 1000
        for rule in schedule.rules where rule.enabled {
            if remaining == 0 { more = true; break }
            let key = rule.id + ":" + rule.revision
            let (items, remains) = reminderOccurrences(rule, after: target.cursors[key].flatMap(reminderDate), through: now, limit: remaining)
            more = more || remains
            for item in items where !known.contains(item.id) {
                guard target.receipt.deliveries.count < 100000 else { target.receipt.error = "Delivery history reached its bound. Open Trove to review retained reminders."; try save(target); return }
                known.insert(item.id); target.receipt.deliveries.append(item)
            }
            target.cursors[key] = remains ? items.last?.dueAt : reminderString(now)
            remaining -= items.count
        }
        try save(target)
        let allowed = target.receipt.permission == "authorized" || target.receipt.permission == "provisional"
        var summary: [String] = []
        let rules = Dictionary(uniqueKeysWithValues: schedule.rules.map { ($0.id, $0) })
        for index in target.receipt.deliveries.indices {
            var item = target.receipt.deliveries[index]
            guard let rule = rules[item.reminderId], rule.revision == item.revision, rule.enabled, item.action != "dismissed" else { continue }
            if item.action == "snoozed", let until = item.snoozedUntil, let due = reminderDate(until), due <= now {
                let key = item.id + ":snooze:" + until
                if target.snoozesSent[key] != true { target.snoozesSent[key] = true; try submit(target, ids: [item.id], identifier: key, title: rule.privateText ? "Trove reminder" : rule.title, body: "A snoozed reminder is ready. Open Trove to review it.", page: rule.pageId, allowed: allowed) }
            } else if item.outcome == "scheduled", item.action == nil || item.action == "" {
                if let due = reminderDate(item.dueAt), now.timeIntervalSince(due) > 15 {
                    if target.episode == nil { target.episode = UUID().uuidString.lowercased(); target.summaryOutcome = nil }
                    let notificationID = "summary:" + schedule.epoch + ":" + target.episode!
                    target.notificationIDs[item.id] = notificationID
                    if let outcome = target.summaryOutcome { item.outcome = outcome; target.receipt.deliveries[index] = item } else { summary.append(item.id) }
                } else { try submit(target, ids: [item.id], identifier: item.id, title: rule.privateText ? "Trove reminder" : rule.title, body: "Open Trove to review your reminder.", page: rule.pageId, allowed: allowed) }
            }
        }
        if !summary.isEmpty, let episode = target.episode {
            target.summaryOutcome = allowed ? "unknown" : "failed"
            try submit(target, ids: summary, identifier: "summary:" + schedule.epoch + ":" + episode, title: "Trove reminders", body: "You have missed reminders. Open Trove to review every occurrence in the reminder inbox.", page: nil, allowed: allowed)
        }
        target.lastTick = reminderString(now)
        if !more { target.episode = nil }
        try save(target)
        reconcileUnknown(target)
    }
    func submit(_ target: ReminderAgentState, ids: [String], identifier: String, title: String, body: String, page: String?, allowed: Bool) throws {
        guard let schedule = schedule else { return }
        let affected = Set(ids)
        for i in target.receipt.deliveries.indices where affected.contains(target.receipt.deliveries[i].id) {
            target.receipt.deliveries[i].outcome = allowed ? "unknown" : "failed"
            target.receipt.deliveries[i].error = allowed ? nil : "Notifications are disabled or permission has not been granted."
            target.notificationIDs[target.receipt.deliveries[i].id] = identifier
        }
        try save(target) // Durable unknown before asking the OS; never blind-retry.
        guard allowed else { return }
        if let outcome = testOutcome {
            testSubmissions.append(identifier)
            if outcome != "unknown" {
                for i in target.receipt.deliveries.indices where affected.contains(target.receipt.deliveries[i].id) { target.receipt.deliveries[i].outcome = outcome }
                if identifier.hasPrefix("summary:") { target.summaryOutcome = outcome }
                try save(target)
            }
            return
        }
        let content = UNMutableNotificationContent(); content.title = title; content.body = body
        if let page = page { content.categoryIdentifier = "reminder"; content.userInfo = ["occurrenceId": ids[0], "pageId": page, "workspaceId": schedule.workspaceId, "epoch": schedule.epoch] }
        pendingSubmissions += 1
        center.add(UNNotificationRequest(identifier: identifier, content: content, trigger: nil)) { error in DispatchQueue.main.async {
            defer { self.pendingSubmissions -= 1 }
            for i in target.receipt.deliveries.indices where affected.contains(target.receipt.deliveries[i].id) {
                target.receipt.deliveries[i].outcome = error == nil ? "submitted" : "failed"
                target.receipt.deliveries[i].error = error == nil ? nil : "The OS rejected notification submission."
            }
            if identifier.hasPrefix("summary:") { target.summaryOutcome = error == nil ? "submitted" : "failed" }
            try? self.save(target)
        } }
    }
    func reconcileUnknown(_ target: ReminderAgentState) {
        if testOutcome != nil { return }
        guard target.receipt.deliveries.contains(where: { $0.outcome == "unknown" }) else { return }
        center.getDeliveredNotifications { notifications in DispatchQueue.main.async {
            let ids = Set(notifications.map { $0.request.identifier })
            for i in target.receipt.deliveries.indices where target.receipt.deliveries[i].outcome == "unknown" {
                if let id = target.notificationIDs[target.receipt.deliveries[i].id], ids.contains(id) { target.receipt.deliveries[i].outcome = "submitted" }
            }
            try? self.save(target)
        } }
    }
    func userNotificationCenter(_ center: UNUserNotificationCenter, didReceive response: UNNotificationResponse, withCompletionHandler complete: @escaping () -> Void) {
        DispatchQueue.main.async {
            defer { complete() }
            let info = response.notification.request.content.userInfo
            guard let epoch = info["epoch"] as? String, epoch == self.schedule?.epoch, let id = info["occurrenceId"] as? String, let index = self.state.receipt.deliveries.firstIndex(where: { $0.id == id }) else {
                if response.actionIdentifier == UNNotificationDefaultActionIdentifier { var main = Bundle.main.bundleURL; for _ in 0..<4 { main.deleteLastPathComponent() }; NSWorkspace.shared.open(main) }; return
            }
            if response.actionIdentifier == "snooze" || response.actionIdentifier == "dismiss" || response.actionIdentifier == UNNotificationDismissActionIdentifier { self.state.receipt.deliveries[index].actionRevision = (self.state.receipt.deliveries[index].actionRevision ?? 0) + 1 }
            if response.actionIdentifier == "snooze" { self.state.receipt.deliveries[index].action = "snoozed"; self.state.receipt.deliveries[index].snoozedUntil = reminderString(Date().addingTimeInterval(900)) }
            else if response.actionIdentifier == "dismiss" || response.actionIdentifier == UNNotificationDismissActionIdentifier { self.state.receipt.deliveries[index].action = "dismissed"; self.state.receipt.deliveries[index].snoozedUntil = nil }
            else if let page = info["pageId"] as? String, let workspace = info["workspaceId"] as? String, UUID(uuidString: page) != nil, UUID(uuidString: workspace) != nil, let url = URL(string: "trove://page/" + page + "?workspace=" + workspace) { NSWorkspace.shared.open(url) }
            try? self.save(self.state)
        }
    }
}

@main struct ReminderMain {
    static func main() {
        if CommandLine.arguments.dropFirst().first == "--test-delivery" {
            let root = FileManager.default.temporaryDirectory.appendingPathComponent("trove-reminder-fixture-" + UUID().uuidString)
            defer { try? FileManager.default.removeItem(at: root) }
            do {
                let agent = try ReminderAgent(root: root, testOutcome: "unknown")
                let rule = ReminderRule(id: "019a1234-5678-7000-8000-000000000001", revision: "1", pageId: "019a1234-5678-7000-8000-000000000002", title: "Private title", startDate: "2020-01-01", localTime: "09:00", timezone: "UTC", frequency: "daily", enabled: true, privateText: true)
                agent.schedule = ReminderSchedule(version: 1, epoch: "019a1234-5678-7000-8000-000000000003", generation: 1, workspaceId: "019a1234-5678-7000-8000-000000000004", active: true, rules: [rule], overrides: [])
                agent.state.receipt.epoch = agent.schedule!.epoch; agent.state.receipt.generation = 1; agent.state.receipt.permission = "authorized"
                let through = reminderDate("2026-01-01T12:00:00Z")!
                for second in 0..<4 { try agent.process(at: through.addingTimeInterval(Double(second))) }
                let firstCount = agent.testSubmissions.count
                let restored = try agent.read("state-" + agent.state.receipt.epoch + ".json", as: ReminderAgentState.self)
                agent.state = restored; try agent.process(at: through.addingTimeInterval(5))
                let retained = agent.state.receipt.deliveries.count
                let unknown = agent.state.receipt.deliveries.filter { $0.outcome == "unknown" }.count
                let output: [String: Any] = ["submissions": firstCount, "afterRestartSubmissions": agent.testSubmissions.count, "retainedOccurrences": retained, "unknownOccurrences": unknown]
                FileHandle.standardOutput.write(try JSONSerialization.data(withJSONObject: output)); return
            } catch { try? FileManager.default.removeItem(at: root); exit(2) }
        }
        if CommandLine.arguments.dropFirst().first == "--test-schedule" {
            do {
                struct TestInput: Decodable { var rule: ReminderRule; var after: String?; var through: String }
                let input = try JSONDecoder().decode(TestInput.self, from: FileHandle.standardInput.readDataToEndOfFile())
                guard reminderRuleValid(input.rule), let through = reminderDate(input.through) else { exit(2) }
                let (items, _) = reminderOccurrences(input.rule, after: input.after.flatMap(reminderDate), through: through)
                FileHandle.standardOutput.write(try JSONEncoder().encode(items)); exit(0)
            } catch { exit(2) }
        }
        guard CommandLine.arguments.count == 1 else { exit(2) }
        do {
            let base = try FileManager.default.url(for: .applicationSupportDirectory, in: .userDomainMask, appropriateFor: nil, create: true)
            let agent = try ReminderAgent(root: base.appendingPathComponent("Trove/reminders", isDirectory: true))
            let app = NSApplication.shared; app.setActivationPolicy(.accessory)
            let timer = Timer.scheduledTimer(withTimeInterval: 2, repeats: true) { _ in agent.tick() }
            agent.tick(); withExtendedLifetime((agent, timer)) { app.run() }
        } catch { exit(1) }
    }
}

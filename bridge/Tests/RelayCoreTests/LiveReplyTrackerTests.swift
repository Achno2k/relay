import Foundation
import Synchronization
import Testing
@testable import RelayCore

/// A settable clock, safe to capture in the tracker's `@Sendable` closure.
final class TestClock: Sendable {
    private let value: Mutex<Date>
    init(_ date: Date = Date()) { value = Mutex(date) }
    func advance(_ seconds: TimeInterval) { value.withLock { $0 = $0.addingTimeInterval(seconds) } }
    func now() -> Date { value.withLock { $0 } }
}

@Suite struct LiveReplyTrackerTests {
    @Test func firstOfferSendsRightAway() async {
        let tracker = LiveReplyTracker(minInterval: 0.25)
        let ev = await tracker.offer(agentId: "a1", text: "Hello")
        #expect(ev == .replyLive(agentId: "a1", text: "Hello", seq: 1))
    }

    @Test func unchangedTextIsNotResent() async {
        let tracker = LiveReplyTracker(minInterval: 0)
        _ = await tracker.offer(agentId: "a1", text: "Hello")
        let ev = await tracker.offer(agentId: "a1", text: "Hello")
        #expect(ev == nil)
    }

    @Test func throttlesWithinTheMinInterval() async {
        let clock = TestClock()
        let tracker = LiveReplyTracker(minInterval: 0.25, clock: clock.now)
        let first = await tracker.offer(agentId: "a1", text: "Hello")
        #expect(first != nil)
        clock.advance(0.1)
        let second = await tracker.offer(agentId: "a1", text: "Hello there")
        #expect(second == nil)  // throttled, even though the text changed
        clock.advance(0.2)
        let third = await tracker.offer(agentId: "a1", text: "Hello there")
        #expect(third == .replyLive(agentId: "a1", text: "Hello there", seq: 2))
    }

    @Test func landedTextSuppressesAMatchingOffer() async {
        let tracker = LiveReplyTracker(minInterval: 0)
        _ = await tracker.offer(agentId: "a1", text: "Hello there")
        let landedEvent = await tracker.landed(agentId: "a1", text: "Hello there, world")
        #expect(landedEvent == .replyLive(agentId: "a1", text: nil, seq: 2))
        // Now that the transcript has caught up, offering the same (or a shorter) tail is suppressed.
        let stale = await tracker.offer(agentId: "a1", text: "Hello there")
        #expect(stale == nil)
    }

    @Test func landedWithNothingLiveSendsNoEvent() async {
        let tracker = LiveReplyTracker(minInterval: 0)
        let ev = await tracker.landed(agentId: "a1", text: "some text")
        #expect(ev == nil)
    }

    @Test func stoppedClearsAnyLiveText() async {
        let tracker = LiveReplyTracker(minInterval: 0)
        _ = await tracker.offer(agentId: "a1", text: "Hello")
        let ev = await tracker.stopped(agentId: "a1")
        #expect(ev == .replyLive(agentId: "a1", text: nil, seq: 2))
        // A second stop is a no-op: nothing to clear.
        let again = await tracker.stopped(agentId: "a1")
        #expect(again == nil)
    }

    @Test func emptyTextNeverOffersAFrame() async {
        let tracker = LiveReplyTracker(minInterval: 0)
        let ev = await tracker.offer(agentId: "a1", text: "")
        #expect(ev == nil)
    }

    @Test func sequenceKeepsIncreasingAcrossOffersAndClears() async {
        let tracker = LiveReplyTracker(minInterval: 0)
        let e1 = await tracker.offer(agentId: "a1", text: "One")
        let e2 = await tracker.offer(agentId: "a1", text: "One two")
        let e3 = await tracker.stopped(agentId: "a1")
        #expect([e1, e2, e3].compactMap { if case .replyLive(_, _, let seq) = $0 { seq } else { nil } } == [1, 2, 3])
    }

    @Test func differentAgentsAreIndependent() async {
        let tracker = LiveReplyTracker(minInterval: 0)
        let a = await tracker.offer(agentId: "a1", text: "Hello")
        let b = await tracker.offer(agentId: "a2", text: "Hello")
        #expect(a == .replyLive(agentId: "a1", text: "Hello", seq: 1))
        #expect(b == .replyLive(agentId: "a2", text: "Hello", seq: 1))
    }
}

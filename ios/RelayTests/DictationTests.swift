import SwiftUI
import Testing
@testable import Relay

/// Stands in for the mic and recognizer: the test yields the events.
@MainActor
private final class FakeEngine: DictationEngine {
    var events: AsyncThrowingStream<DictationEvent, any Error>.Continuation?
    var finished = 0
    var cancelled = 0

    func start() -> AsyncThrowingStream<DictationEvent, any Error> {
        let (stream, events) = AsyncThrowingStream.makeStream(of: DictationEvent.self)
        self.events = events
        return stream
    }

    func finish() { finished += 1 }
    func cancel() { cancelled += 1 }
}

@MainActor
private final class Field {
    var text: String
    init(_ text: String) { self.text = text }
    var binding: Binding<String> { Binding(get: { self.text }, set: { self.text = $0 }) }
}

/// Lets the dictation task consume what was yielded (everything runs on the main actor).
@MainActor
private func settle() async {
    for _ in 0..<20 { await Task.yield() }
}

@Suite("Dictation")
@MainActor
struct DictationTests {
    @Test func joinsWithASpaceOnlyWhenNeeded() {
        #expect(DictationInsertion.join("", "hello") == "hello")
        #expect(DictationInsertion.join("", "  hello") == "hello")
        #expect(DictationInsertion.join("fix", "the build") == "fix the build")
        #expect(DictationInsertion.join("fix ", "the build") == "fix the build")
        #expect(DictationInsertion.join("fix\n", "the build") == "fix\nthe build", "a new line is kept, no space after it")
        #expect(DictationInsertion.join("fix", " the build") == "fix the build", "no double space")
        #expect(DictationInsertion.join("Done", ".") == "Done.", "punctuation sticks to the word")
        #expect(DictationInsertion.join("Done", ", then") == "Done, then")
        #expect(DictationInsertion.join("fix", "") == "fix", "nothing heard adds no space")
        #expect(DictationInsertion.join("fix", "  ") == "fix")
    }

    @Test func appendsToTheDraftAndReplacesVolatileText() {
        var insertion = DictationInsertion(base: "Please")
        var field = "Please"
        field = insertion.apply("run", isFinal: false, current: field)!
        #expect(field == "Please run")
        field = insertion.apply("run the", isFinal: false, current: field)!
        #expect(field == "Please run the", "a volatile result replaces the last one")
        field = insertion.apply("Run the tests.", isFinal: true, current: field)!
        #expect(field == "Please Run the tests.")
        field = insertion.apply("Then", isFinal: false, current: field)!
        #expect(field == "Please Run the tests. Then")
        field = insertion.apply(" Then push.", isFinal: true, current: field)!
        #expect(field == "Please Run the tests. Then push.")
        #expect(insertion.written == field && insertion.base == "Please")
    }

    @Test func anEditInTheFieldStopsInsertion() {
        var insertion = DictationInsertion(base: "")
        let field = insertion.apply("hello", isFinal: false, current: "")!
        #expect(insertion.apply("hello world", isFinal: false, current: field + "!") == nil, "the user's edit wins")
    }

    @Test func streamsIntoTheFieldAndStopsOnTap() async {
        let engine = FakeEngine()
        let dictation = Dictation(makeEngine: { engine })
        let field = Field("Check")
        dictation.toggle(field.binding)
        #expect(dictation.phase == .starting)

        engine.events?.yield(.downloading(nil))
        await settle()
        #expect(dictation.phase == .downloading(nil))
        engine.events?.yield(.downloading(0.5))
        engine.events?.yield(.listening)
        engine.events?.yield(.level(0.6))
        engine.events?.yield(.text("the logs", isFinal: false))
        await settle()
        #expect(dictation.phase == .listening && dictation.level == 0.6)
        #expect(field.text == "Check the logs")

        dictation.toggle(field.binding)
        #expect(engine.finished == 1 && dictation.phase == .idle && dictation.level == 0)
        // The last words still arrive after stop.
        engine.events?.yield(.text("the logs please", isFinal: true))
        engine.events?.yield(.level(0.9))
        engine.events?.finish()
        await settle()
        #expect(field.text == "Check the logs please")
        #expect(dictation.phase == .idle && dictation.level == 0, "a late level doesn't turn it back on")
    }

    @Test func cancelDropsLateWords() async {
        let engine = FakeEngine()
        let dictation = Dictation(makeEngine: { engine })
        let field = Field("")
        dictation.start(field.binding)
        engine.events?.yield(.listening)
        engine.events?.yield(.text("ship it", isFinal: false))
        await settle()
        #expect(field.text == "ship it")

        // Send: the composer cancels, then clears the field.
        dictation.cancel()
        field.text = ""
        engine.events?.yield(.text("ship it now", isFinal: true))
        await settle()
        #expect(field.text == "", "nothing lands after send")
        #expect(engine.cancelled == 1 && !dictation.isActive)
    }

    @Test func typingWhileListeningStops() async {
        let engine = FakeEngine()
        let dictation = Dictation(makeEngine: { engine })
        let field = Field("")
        dictation.start(field.binding)
        engine.events?.yield(.listening)
        engine.events?.yield(.text("hello", isFinal: false))
        await settle()
        dictation.noteEdit(field.text)
        #expect(dictation.isActive, "its own write isn't an edit")

        field.text = "hello there"
        dictation.noteEdit(field.text)
        #expect(!dictation.isActive && engine.cancelled == 1)
        engine.events?.yield(.text("hello world", isFinal: true))
        await settle()
        #expect(field.text == "hello there")
    }

    @Test func tappingWhileStartingCancels() {
        let engine = FakeEngine()
        let dictation = Dictation(makeEngine: { engine })
        let field = Field("")
        dictation.toggle(field.binding)
        dictation.toggle(field.binding)
        #expect(engine.cancelled == 1 && engine.finished == 0 && !dictation.isActive)
    }

    @Test func deniedPermissionShowsASettingsNotice() async {
        let engine = FakeEngine()
        let dictation = Dictation(makeEngine: { engine })
        let field = Field("draft")
        dictation.start(field.binding)
        engine.events?.finish(throwing: DictationError.denied("Relay can't use the microphone."))
        await settle()
        #expect(dictation.notice == .init(message: "Relay can't use the microphone.", opensSettings: true))
        #expect(!dictation.isActive && field.text == "draft")

        // Trying again clears it.
        dictation.start(field.binding)
        #expect(dictation.notice == nil)
        engine.events?.finish(throwing: DictationError.unavailable)
        await settle()
        #expect(dictation.notice?.opensSettings == false)
        #expect(dictation.notice?.message == "Dictation isn't available on this device.")
    }

    @Test func aNewStartCancelsTheOldOne() async {
        var engines: [FakeEngine] = []
        let dictation = Dictation(makeEngine: {
            let engine = FakeEngine()
            engines.append(engine)
            return engine
        })
        let field = Field("")
        dictation.start(field.binding)
        engines[0].events?.yield(.listening)
        await settle()
        dictation.finish()
        dictation.start(field.binding)
        #expect(engines.count == 2 && engines[0].cancelled == 1)
        engines[0].events?.yield(.text("old", isFinal: true))
        engines[1].events?.yield(.text("new", isFinal: false))
        await settle()
        #expect(field.text == "new")
    }
}

#if DEBUG
import Foundation
import HerdKit

/// Synthetic chats for the fixture agents that `messages.json` doesn't cover.
enum MockChats {
    static func all(fixtureMessages: MessagePage) -> [String: [Message]] {
        [
            "w1:p1": fixtureMessages.messages,
            "w1:p2": flakyTests,
            "w2:p1": landing,
            "w2:p3": codex,
        ]
    }

    private static func at(_ minutesAgo: Double) -> Date { Date().addingTimeInterval(-minutesAgo * 60) }

    static let flakyTests: [Message] = [
        Message(id: "f1", role: .user, createdAt: at(9), blocks: [
            .text("checkout tests fail about 1 run in 5 on CI. Find out why and fix it."),
        ]),
        Message(id: "f2", role: .assistant, createdAt: at(8.8), blocks: [
            .thinking("Flaky 1 in 5 smells like ordering or time. Look at the fixtures first."),
            .toolCall(ToolCall(id: "f-t1", name: "Glob", summary: "Found tests/checkout/**/*.py")),
            .toolResult(ToolResult(toolCallId: "f-t1", isError: false, preview: "tests/checkout/test_cart.py\ntests/checkout/test_pay.py")),
            .toolCall(ToolCall(id: "f-t2", name: "Read", summary: "Read tests/checkout/conftest.py")),
            .toolResult(ToolResult(toolCallId: "f-t2", isError: false, preview: "@pytest.fixture\ndef cart(db): ...")),
            .text("""
            The `cart` fixture shares one database row across tests, and `test_pay.py` mutates it. \
            When pytest-xdist schedules both files on the same worker, the order decides who wins.

            I'll give each test its own cart and run the suite a few times to confirm.
            """),
            .toolCall(ToolCall(id: "f-t3", name: "Edit", summary: "Edited tests/checkout/conftest.py")),
            .toolResult(ToolResult(toolCallId: "f-t3", isError: false, preview: "The file tests/checkout/conftest.py has been updated.")),
        ]),
    ]

    static let landing: [Message] = [
        Message(id: "l1", role: .user, createdAt: at(90), blocks: [
            .text("Make the hero headline shorter and swap the CTA to \"Start free\"."),
        ]),
        Message(id: "l2", role: .assistant, createdAt: at(89), blocks: [
            .toolCall(ToolCall(id: "l-t1", name: "Read", summary: "Read src/components/Hero.tsx")),
            .toolResult(ToolResult(toolCallId: "l-t1", isError: false, preview: "export function Hero() {")),
            .toolCall(ToolCall(id: "l-t2", name: "Edit", summary: "Edited src/components/Hero.tsx")),
            .toolResult(ToolResult(toolCallId: "l-t2", isError: false, preview: "The file src/components/Hero.tsx has been updated.")),
            .text("""
            ## Done

            - Headline is now **Ship the storefront, not the plumbing.** (7 words, down from 15)
            - CTA reads `Start free` and still links to `/signup`

            ```tsx
            <Button href="/signup" size="lg">
              Start free
            </Button>
            ```
            """),
        ]),
    ]

    /// Codex has no transcript here, so the bridge falls back to reading the screen.
    static let codex: [Message] = [
        Message(id: "c1", role: .assistant, createdAt: at(170), blocks: [
            .text("Updated `package.json` scripts and removed the unused `lint:css` task. All checks pass."),
        ]),
    ]
}
#endif

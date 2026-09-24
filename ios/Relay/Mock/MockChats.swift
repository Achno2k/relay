#if DEBUG
import Foundation
import HerdKit

/// Synthetic chats for the fixture agents that `messages.json` doesn't cover.
enum MockChats {
    static func all(fixtureMessages: MessagePage) -> [String: [Message]] {
        [
            "w1:p1": fixtureMessages.messages,
            "w1:p2": flakyTests,
            "w2:p1": longHistory + landing,
            "w2:p3": codex,
            "w2:p4": pi,
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

    /// Enough turns to need a second page and to show whether a chat opens at the bottom.
    static let longHistory: [Message] = (0..<32).flatMap { i -> [Message] in
        let minutes = Double(400 - i * 9)
        return [
            Message(id: "h\(i)u", role: .user, createdAt: at(minutes), blocks: [
                .text("Tweak \(i + 1): tighten the spacing in section \(i % 5 + 1)."),
            ]),
            Message(id: "h\(i)a", role: .assistant, createdAt: at(minutes - 1), blocks: [
                .toolCall(ToolCall(id: "h\(i)t", name: "Edit", summary: "Edited src/styles/section\(i % 5 + 1).css")),
                .toolResult(ToolResult(toolCallId: "h\(i)t", isError: false, preview: "Updated.")),
                .text(i % 3 == 0
                    ? "Reduced the gap from `48px` to `32px` and aligned the heading baseline.\n\n- Mobile keeps `24px`\n- Desktop uses the new token\n\n```css\n.section-\(i % 5 + 1) {\n  gap: var(--space-8);\n  padding-block: var(--space-12);\n}\n```\n\nThe hero and footer already used the token, so nothing else changed. I checked the page at 375, 768 and 1280 pixels wide and the rhythm is consistent now. If you want the tighter spacing on the pricing table as well, say so and I'll apply the same change there."
                    : "Done. Section \(i % 5 + 1) now uses the `space-8` token."),
            ]),
        ]
    }

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

    static let pi: [Message] = [
        Message(id: "p1", role: .user, createdAt: at(26), blocks: [.text("Remove the stale pages under docs/legacy.")]),
        Message(id: "p2", role: .assistant, createdAt: at(25), blocks: [
            .toolCall(ToolCall(id: "p-t1", name: "bash", summary: "Ran git rm -r docs/legacy")),
            .toolResult(ToolResult(toolCallId: "p-t1", isError: false, preview: "rm 'docs/legacy/intro.md'")),
            .text("Removed 6 pages from `docs/legacy` and fixed the two links that pointed at them."),
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

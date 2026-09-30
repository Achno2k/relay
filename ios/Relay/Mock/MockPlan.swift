#if DEBUG
import Foundation
import RelayKit

/// `-mockPlan` (B7): w1:p2 ("Fix flaky checkout tests", Claude in plan mode) ends its chat with a plan
/// written and `ExitPlanMode` called, and is blocked on Claude's plan approval, which carries the plan.
/// Behind a flag so the default mock (and every test built on its approval) stays as it was.
enum MockPlan {
    static let agentId = "w1:p2"

    static let markdown = """
    # Fix flaky checkout tests

    ## Why
    The `cart` fixture shares one database row across tests, and `test_pay.py` mutates it. \
    When pytest-xdist puts both files on the same worker, the order decides who wins.

    ## Steps
    1. Give each test its own `cart` fixture (function scope, fresh row).
    2. Drop the `@retry(3)` decorator from `test_pay.py`.
    3. Run the suite 20 times with `-n 4` to confirm.
    4. Note the fixture rule in `tests/README.md`.

    ## Out of scope
    - Moving the tests to Postgres containers.
    - The slow `test_refunds.py` (separate issue).
    """

    static let approval = Approval(
        agentId: agentId,
        question: "Claude has written up a plan and is ready to execute. Would you like to proceed?",
        options: [
            ApprovalOption(label: "Yes, and auto-accept edits", keys: ["1"]),
            ApprovalOption(label: "Yes, and manually approve edits", keys: ["2"]),
            ApprovalOption(label: "No, keep planning", keys: ["3"]),
        ],
        plan: markdown
    )

    /// Appended after the chat's last message: the plan file written, then `ExitPlanMode` waiting on approval.
    static var messages: [Message] {
        let at = Date().addingTimeInterval(-60)
        return [
            Message(id: "f-plan", role: .assistant, createdAt: at, blocks: [
                .text("I have what I need. Here's the plan."),
                .toolCall(ToolCall(
                    id: "f-plan-write", name: "Write", summary: "Wrote quiet-orange-kettle.md",
                    edit: ToolEdit(kind: .write, content: markdown)
                )),
                .toolResult(ToolResult(toolCallId: "f-plan-write", isError: false, preview: "File created successfully at: quiet-orange-kettle.md")),
                .toolCall(ToolCall(id: "f-plan-exit", name: "ExitPlanMode", summary: "ExitPlanMode", input: "{}", plan: markdown)),
            ]),
        ]
    }
}
#endif

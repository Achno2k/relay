import RelayKit
import SwiftUI

/// Bottom sheet with one full-width glass button per option, sized to its content.
/// A free-text option ("Type something.") swaps the buttons for a text field.
/// A plan (Claude's ExitPlanMode) opens it tall: the plan scrolls, the options stay pinned below it.
struct ApprovalSheet: View {
    let approval: Approval
    let agentTitle: String
    let onChoose: (ApprovalOption, String?) -> Void

    @State private var height: CGFloat = 360
    @State private var freeTextOption: ApprovalOption?
    @State private var answer = ""
    @FocusState private var answerFocused: Bool

    private var plan: String? {
        guard let plan = approval.plan, !plan.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else { return nil }
        return plan
    }

    var body: some View {
        Group {
            if let plan {
                planLayout(plan)
            } else {
                ScrollView {
                    VStack(alignment: .leading, spacing: 24) {
                        header
                        answers
                    }
                    .padding(.horizontal, 24)
                    .padding(.top, 28)
                    .padding(.bottom, 12)
                    .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { height = $0 }
                }
                .scrollBounceBehavior(.basedOnSize)
                .presentationDetents([.height(height)])
            }
        }
        .presentationDragIndicator(.visible)
        .animation(.smooth, value: freeTextOption)
        .onChange(of: approval) {
            freeTextOption = nil
            answer = ""
        }
    }

    @ViewBuilder
    private var answers: some View {
        if let option = freeTextOption {
            freeTextField(option)
        } else {
            VStack(spacing: 10) {
                ForEach(Array(approval.options.enumerated()), id: \.offset) { i, option in
                    optionButton(option, prominent: i == 0)
                }
            }
        }
    }

    private func planLayout(_ plan: String) -> some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 20) {
                header
                MarkdownView(plan)
                    .textSelection(.enabled)
                    .padding(16)
                    .background(Color(.tertiarySystemFill), in: .rect(cornerRadius: 20))
                    .accessibilityIdentifier("approvalPlan")
            }
            .padding(.horizontal, 24)
            .padding(.top, 28)
            .padding(.bottom, 12)
        }
        // A bar, not a plain inset: the scroll edge effect then covers the plan under the pinned options.
        .scrollEdgeEffectStyle(.hard, for: .bottom)
        .safeAreaBar(edge: .bottom, spacing: 0) {
            answers
                .padding(.horizontal, 24)
                .padding(.top, 12)
                .padding(.bottom, 8)
        }
        .presentationDetents([.large])
    }

    private var header: some View {
        VStack(alignment: .leading, spacing: 10) {
            VStack(alignment: .leading, spacing: 4) {
                Label(agentTitle, systemImage: "hand.raised.fill")
                    .font(.subheadline.weight(.semibold))
                    .foregroundStyle(.orange)
                    .lineLimit(1)
                if let step = approval.step {
                    Text(stepText(step))
                        .font(.footnote.weight(.medium))
                        .foregroundStyle(.secondary)
                        .accessibilityIdentifier("approvalStep")
                }
            }
            Text(MarkdownParser.inline(
                approval.question,
                codeFont: .system(.title3, design: .monospaced).weight(.regular),
                codeBackground: Color(.quaternarySystemFill)
            ))
            .font(.title3.weight(.semibold))
            .fixedSize(horizontal: false, vertical: true)
            .accessibilityIdentifier("approvalQuestion")
        }
    }

    private func stepText(_ step: ApprovalStep) -> String {
        // The last tab is Claude's review screen, not a question.
        if step.index == step.count, step.title == "Submit" { return "Review answers" }
        let position = "Question \(step.index) of \(step.count)"
        guard let title = step.title, !title.isEmpty else { return position }
        return "\(position) · \(title)"
    }

    @ViewBuilder
    private func optionButton(_ option: ApprovalOption, prominent: Bool) -> some View {
        let button = Button {
            if option.isFreeText {
                freeTextOption = option
                answerFocused = true
            } else {
                onChoose(option, nil)
            }
        } label: {
            HStack(spacing: 8) {
                if option.isFreeText {
                    Image(systemName: "character.cursor.ibeam")
                }
                // Never truncated: at large text sizes two lines cut "Yes, and don't ask again for …"
                // before saying what for. The sheet scrolls when the options outgrow it.
                Text(option.label)
                    .fixedSize(horizontal: false, vertical: true)
            }
            .font(.body.weight(.semibold))
            .frame(maxWidth: .infinity)
            .padding(.vertical, 6)
        }
        .controlSize(.large)
        if prominent {
            button.buttonStyle(.glassProminent)
        } else {
            button.buttonStyle(.glass)
        }
    }

    private func freeTextField(_ option: ApprovalOption) -> some View {
        let trimmed = answer.trimmingCharacters(in: .whitespacesAndNewlines)
        return VStack(alignment: .leading, spacing: 12) {
            HStack(alignment: .bottom, spacing: 10) {
                TextField("Your answer", text: $answer, axis: .vertical)
                    .lineLimit(1...5)
                    .focused($answerFocused)
                    .padding(.horizontal, 16)
                    .padding(.vertical, 12)
                    .glassEffect(.regular.interactive(), in: .rect(cornerRadius: 22))
                    .accessibilityIdentifier("approvalAnswer")
                    .onSubmit { if !trimmed.isEmpty { onChoose(option, trimmed) } }
                Button {
                    onChoose(option, trimmed)
                } label: {
                    Image(systemName: "arrow.up")
                        .font(.system(size: 17, weight: .bold))
                        .frame(width: 44, height: 44)
                }
                .buttonStyle(.glassProminent)
                .buttonBorderShape(.circle)
                .disabled(trimmed.isEmpty)
                .accessibilityLabel("Send answer")
            }
            Button("Back to options") {
                freeTextOption = nil
                answerFocused = false
            }
            .font(.subheadline.weight(.medium))
            .foregroundStyle(.secondary)
        }
        .transition(.opacity.combined(with: .move(edge: .bottom)))
    }
}

/// Inline card above the composer that reopens the sheet.
struct ApprovalCard: View {
    let approval: Approval
    let action: () -> Void

    private var hasPlan: Bool { !(approval.plan ?? "").trimmingCharacters(in: .whitespacesAndNewlines).isEmpty }

    var body: some View {
        Button(action: action) {
            HStack(spacing: 12) {
                Image(systemName: "hand.raised.fill")
                    .foregroundStyle(.orange)
                    .frame(width: 32, height: 32)
                    .background(.orange.opacity(0.15), in: .circle)
                VStack(alignment: .leading, spacing: 2) {
                    Text(hasPlan ? "Review the plan" : "Needs your approval")
                        .font(.subheadline.weight(.semibold))
                    Text(MarkdownParser.inline(approval.question, codeBackground: Color(.quaternarySystemFill)))
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                }
                Spacer(minLength: 0)
                Image(systemName: "chevron.up")
                    .font(.footnote.weight(.semibold))
                    .foregroundStyle(.tertiary)
            }
            .padding(12)
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .glassEffect(.regular.interactive(), in: .rect(cornerRadius: 22))
        .accessibilityIdentifier("approvalCard")
    }
}

import HerdKit
import SwiftUI

/// Bottom sheet with one full-width glass button per option.
struct ApprovalSheet: View {
    let approval: Approval
    let agentTitle: String
    let onChoose: (ApprovalOption) -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 20) {
            VStack(alignment: .leading, spacing: 10) {
                Label(agentTitle, systemImage: "hand.raised.fill")
                    .font(.subheadline.weight(.semibold))
                    .foregroundStyle(.orange)
                    .lineLimit(1)
                Text(MarkdownParser.inline(approval.question))
                    .font(.title3.weight(.semibold))
                    .fixedSize(horizontal: false, vertical: true)
            }
            Spacer(minLength: 0)
            VStack(spacing: 10) {
                ForEach(Array(approval.options.enumerated()), id: \.offset) { i, option in
                    optionButton(option, prominent: i == 0)
                }
            }
        }
        .padding(.horizontal, 24)
        .padding(.top, 28)
        .padding(.bottom, 12)
        .presentationDetents([.medium])
        .presentationDragIndicator(.visible)
    }

    @ViewBuilder
    private func optionButton(_ option: ApprovalOption, prominent: Bool) -> some View {
        let button = Button {
            onChoose(option)
        } label: {
            Text(option.label)
                .font(.body.weight(.semibold))
                .lineLimit(2)
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
}

/// Inline card above the composer that reopens the sheet.
struct ApprovalCard: View {
    let question: String
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            HStack(spacing: 12) {
                Image(systemName: "hand.raised.fill")
                    .foregroundStyle(.orange)
                    .frame(width: 32, height: 32)
                    .background(.orange.opacity(0.15), in: .circle)
                VStack(alignment: .leading, spacing: 2) {
                    Text("Needs your approval")
                        .font(.subheadline.weight(.semibold))
                    Text(MarkdownParser.inline(question))
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
    }
}

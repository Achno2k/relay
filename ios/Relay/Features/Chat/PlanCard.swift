import SwiftUI

/// Claude's plan from plan mode (ExitPlanMode), as a card in the transcript: a few lines with a fade,
/// tap to read it all. Stays in history after the plan is approved or rejected (B7).
struct PlanCard: View {
    let markdown: String
    @State private var expanded: Bool
    @State private var fullHeight: CGFloat = 0

    private static let collapsedHeight: CGFloat = 180

    init(markdown: String, expanded: Bool = false) {
        self.markdown = markdown
        _expanded = State(initialValue: expanded)
    }

    private var isLong: Bool { fullHeight > Self.collapsedHeight + 40 }

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Label("Plan", systemImage: "list.bullet.clipboard")
                .font(.subheadline.weight(.semibold))
                .foregroundStyle(.secondary)
            MarkdownView(markdown)
                .textSelection(.enabled)
                .fixedSize(horizontal: false, vertical: true)
                .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { fullHeight = $0 }
                .frame(maxHeight: expanded || !isLong ? nil : Self.collapsedHeight, alignment: .top)
                .clipped()
                .mask {
                    if expanded || !isLong {
                        Rectangle()
                    } else {
                        LinearGradient(stops: [.init(color: .black, location: 0.6), .init(color: .clear, location: 1)],
                                       startPoint: .top, endPoint: .bottom)
                    }
                }
            if isLong {
                Button {
                    withAnimation(.snappy) { expanded.toggle() }
                } label: {
                    HStack(spacing: 4) {
                        Text(expanded ? "Show less" : "Show full plan")
                        Image(systemName: "chevron.down")
                            .font(.caption.weight(.semibold))
                            .rotationEffect(.degrees(expanded ? 180 : 0))
                    }
                    .font(.subheadline.weight(.medium))
                    .foregroundStyle(.secondary)
                    .contentShape(.rect)
                }
                .buttonStyle(.plain)
                .accessibilityIdentifier("planToggle")
            }
        }
        .padding(16)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Color(.secondarySystemBackground), in: .rect(cornerRadius: 20))
        .accessibilityIdentifier("planCard")
    }
}

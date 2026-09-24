import RelayKit
import SwiftUI

/// A brand-new agent with nothing said yet: what it is and where, like ChatGPT's empty chat.
struct AgentEmptyState: View {
    let agent: Agent
    /// "Opus 5.5 · Auto", when known.
    let subtitle: String?

    var body: some View {
        VStack(spacing: 14) {
            KindIcon(kind: agent.kind)
                .padding(.bottom, 6)
            Text("New \(KindIcon.name(agent.kind)) chat in \(agent.workspaceName)")
                .font(.title3.weight(.semibold))
                .multilineTextAlignment(.center)
            if let subtitle {
                Text(subtitle)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            }
            Text("Send a message to get started.")
                .font(.subheadline)
                .foregroundStyle(.tertiary)
                .padding(.top, 2)
        }
        .padding(.horizontal, 32)
        .frame(maxWidth: .infinity)
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier("emptyAgentChat")
    }
}

/// The agent kind's mark in a glass circle.
struct KindIcon: View {
    let kind: String

    static func name(_ kind: String) -> String {
        switch kind {
        case "claude": "Claude"
        case "codex": "Codex"
        case "pi": "pi"
        default: kind.capitalized
        }
    }

    var body: some View {
        Group {
            switch kind {
            case "pi":
                Text("π").font(.system(size: 30, weight: .semibold, design: .serif))
            case "codex":
                Image(systemName: "chevron.left.forwardslash.chevron.right").font(.system(size: 24, weight: .semibold))
            case "claude":
                Image(systemName: "sparkle").font(.system(size: 28, weight: .semibold))
            default:
                Image(systemName: "cpu").font(.system(size: 26, weight: .semibold))
            }
        }
        .foregroundStyle(.tint)
        .frame(width: 68, height: 68)
        .glassEffect(.regular.tint(.accentColor.opacity(0.12)), in: .circle)
        .accessibilityHidden(true)
    }
}

/// For kinds without a transcript: the bridge's read of the terminal, shown as a styled card
/// rather than a raw code fence.
struct LiveScreenCard: View {
    let text: String

    /// The bridge wraps the screen in a Markdown fence; the card is the frame instead.
    nonisolated static func screenText(_ markdown: String) -> String {
        markdown
            .components(separatedBy: "\n")
            .filter { !$0.trimmingCharacters(in: .whitespaces).hasPrefix("```") }
            .joined(separator: "\n")
            .trimmingCharacters(in: .newlines)
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: 6) {
                Image(systemName: "display")
                Text("Live screen")
                Spacer()
                Text("No transcript for this agent")
                    .foregroundStyle(.tertiary)
            }
            .font(.caption.weight(.medium))
            .foregroundStyle(.secondary)
            .padding(.horizontal, 14)
            .padding(.vertical, 10)

            ScrollView(.horizontal, showsIndicators: false) {
                Text(text)
                    .font(.system(.footnote, design: .monospaced))
                    .lineSpacing(2)
                    .textSelection(.enabled)
                    .fixedSize(horizontal: true, vertical: false)
                    .padding(.horizontal, 14)
                    .padding(.bottom, 14)
            }
        }
        .background(Color(.secondarySystemBackground), in: .rect(cornerRadius: 18))
        .overlay { RoundedRectangle(cornerRadius: 18).strokeBorder(Color(.separator).opacity(0.4), lineWidth: 0.5) }
        // .contain, not .combine: combining drops the scrolled screen text, so VoiceOver couldn't read it.
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("liveScreen")
    }
}

import SwiftUI
import UIKit

/// The mic in the composer field. Listening, it turns into a stop button with a ring that follows the mic level.
struct DictationButton: View {
    let dictation: Dictation
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            ZStack {
                switch dictation.phase {
                case .idle:
                    Image(systemName: "mic")
                        .font(.system(size: 17, weight: .regular))
                        .foregroundStyle(.secondary)
                case .starting:
                    ProgressView().controlSize(.small)
                case .downloading(let fraction):
                    Circle().stroke(.quaternary, lineWidth: 2.5)
                    Circle()
                        .trim(from: 0, to: fraction ?? 0.05)
                        .stroke(.primary, style: StrokeStyle(lineWidth: 2.5, lineCap: .round))
                        .rotationEffect(.degrees(-90))
                        .animation(.smooth, value: fraction)
                    Image(systemName: "stop.fill").font(.system(size: 8, weight: .bold))
                case .listening:
                    Circle()
                        .fill(.red.opacity(0.25))
                        .scaleEffect(1 + CGFloat(dictation.level) * 0.5)
                        .animation(.easeOut(duration: 0.12), value: dictation.level)
                    Circle().fill(.red)
                    Image(systemName: "stop.fill")
                        .font(.system(size: 10, weight: .bold))
                        .foregroundStyle(.white)
                }
            }
            .frame(width: 26, height: 26)
            .frame(width: 44, height: 44)
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .animation(.smooth(duration: 0.25), value: dictation.phase)
        .sensoryFeedback(.impact(weight: .light), trigger: dictation.phase == .listening)
        .accessibilityLabel(dictation.isActive ? "Stop dictation" : "Start dictation")
        .accessibilityValue(value)
        .accessibilityIdentifier("composerMic")
    }

    private var value: String {
        switch dictation.phase {
        case .idle: ""
        case .starting: "Starting"
        case .downloading: "Downloading speech model"
        case .listening: "Listening"
        }
    }
}

/// One line above the composer: the model download, or why dictation didn't start (with a Settings link).
struct DictationNotice: View {
    let dictation: Dictation
    @Environment(\.openURL) private var openURL

    var body: some View {
        if case .downloading(let fraction) = dictation.phase {
            line(Text("Downloading speech model…\(fraction.map { " \(Int($0 * 100))%" } ?? "")"))
        } else if let notice = dictation.notice {
            line(Text(notice.message)) {
                if notice.opensSettings, let url = URL(string: UIApplication.openSettingsURLString) {
                    Button("Settings") { openURL(url) }
                        .font(.footnote.weight(.semibold))
                }
                Button {
                    dictation.notice = nil
                } label: {
                    Image(systemName: "xmark").font(.caption.weight(.semibold))
                }
                .foregroundStyle(.secondary)
                .accessibilityLabel("Dismiss")
            }
        }
    }

    private func line(_ text: Text, @ViewBuilder actions: () -> some View = { EmptyView() }) -> some View {
        HStack(spacing: 12) {
            text
                .font(.footnote)
                .foregroundStyle(.secondary)
                .monospacedDigit()
            actions()
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 8)
        .glassEffect(.regular, in: .capsule)
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("dictationNotice")
    }
}

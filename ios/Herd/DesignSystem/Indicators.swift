import HerdKit
import SwiftUI

/// Highlight sweeping across text, like ChatGPT's "Thinking…" label.
struct Shimmer: ViewModifier {
    @State private var phase: CGFloat = 0
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    func body(content: Content) -> some View {
        content
            .foregroundStyle(.secondary)
            .overlay {
                if !reduceMotion {
                    content
                        .foregroundStyle(.primary)
                        .mask {
                            GeometryReader { geo in
                                LinearGradient(
                                    colors: [.clear, .black, .clear],
                                    startPoint: .leading, endPoint: .trailing
                                )
                                .frame(width: geo.size.width * 0.5)
                                .offset(x: -geo.size.width * 0.5 + phase * geo.size.width * 1.5)
                            }
                        }
                }
            }
            .onAppear {
                withAnimation(.linear(duration: 1.6).repeatForever(autoreverses: false)) { phase = 1 }
            }
    }
}

extension View {
    func shimmer() -> some View { modifier(Shimmer()) }
}

/// ChatGPT's "working" blob: a solid dot breathing in and out.
struct PulsingDot: View {
    var size: CGFloat = 14

    var body: some View {
        Circle()
            .fill(.primary)
            .frame(width: size, height: size)
            .phaseAnimator([false, true]) { dot, big in
                dot.scaleEffect(big ? 1 : 0.65).opacity(big ? 1 : 0.55)
            } animation: { _ in
                .easeInOut(duration: 0.75)
            }
            .accessibilityLabel("Working")
    }
}

enum RelativeTime {
    /// "now", "4m", "2h", "Yesterday", "Mon", "12 Sep".
    static func short(_ date: Date, now: Date = Date()) -> String {
        let seconds = now.timeIntervalSince(date)
        if seconds < 60 { return "now" }
        if seconds < 3600 { return "\(Int(seconds / 60))m ago" }
        if seconds < 86_400, Calendar.current.isDateInToday(date) { return "\(Int(seconds / 3600))h ago" }
        if Calendar.current.isDateInYesterday(date) { return "Yesterday" }
        if seconds < 6 * 86_400 { return date.formatted(.dateTime.weekday(.abbreviated)) }
        return date.formatted(.dateTime.day().month(.abbreviated))
    }

    /// Sidebar style: "now", "10m", "2h", "3d", "12 Sep".
    static func compact(_ date: Date, now: Date = Date()) -> String {
        let seconds = max(0, now.timeIntervalSince(date))
        if seconds < 60 { return "now" }
        if seconds < 3600 { return "\(Int(seconds / 60))m" }
        if seconds < 86_400 { return "\(Int(seconds / 3600))h" }
        if seconds < 7 * 86_400 { return "\(Int(seconds / 86_400))d" }
        return date.formatted(.dateTime.day().month(.abbreviated))
    }

    /// "42s", "3m 5s".
    static func duration(_ interval: TimeInterval) -> String {
        let s = max(1, Int(interval.rounded()))
        if s < 60 { return "\(s)s" }
        let m = s / 60
        return s % 60 == 0 ? "\(m)m" : "\(m)m \(s % 60)s"
    }
}

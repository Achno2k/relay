import RelayKit
import SwiftUI

/// "Now": every running chat across projects in one card, newest first. Shows `SidebarModel.nowCap`
/// until "Show all N" opens the rest (remembered). Nothing at all when no agent is working.
struct SidebarNowCard: View {
    let store: AppStore
    let onSelect: (String) -> Void

    @AppStorage("sidebarNowShowAll", store: AppDefaults.standard) private var showAll = false
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        let now = store.sidebar.grouped().now
        if !now.isEmpty {
            let overflow = now.count > SidebarModel.nowCap
            let shown = showAll || !overflow ? now : Array(now.prefix(SidebarModel.nowCap))
            Section {
                ForEach(shown) { agent in
                    SessionRow(
                        agent: agent, store: store, showsProject: true, onSelect: onSelect,
                        separator: overflow || agent.id != shown.last?.id
                    )
                }
                if overflow {
                    showAllRow(count: now.count)
                }
            } header: {
                SidebarSectionHeader(top: 16) {
                    Text("Now")
                        .font(.headline)
                        .accessibilityAddTraits(.isHeader)
                    Spacer(minLength: 8)
                    Text("\(now.count) running")
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }
                .accessibilityElement(children: .combine)
                .accessibilityIdentifier("nowHeader")
            }
        }
    }

    private func showAllRow(count: Int) -> some View {
        Button {
            withAnimation(reduceMotion ? nil : .smooth) { showAll.toggle() }
        } label: {
            HStack(spacing: 8) {
                Text(showAll ? "Show fewer" : "Show all \(count)")
                    .font(.body)
                    .foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
                Spacer(minLength: 0)
                Image(systemName: "chevron.down")
                    .font(.footnote.weight(.bold))
                    .foregroundStyle(.tertiary)
                    .rotationEffect(.degrees(showAll ? 180 : 0))
            }
            // Lines up with the titles above it: 18 pt inset, 18 pt glyph, 14 pt gap.
            .padding(.leading, 32)
            .padding(.vertical, 8)
            .frame(minHeight: 50)
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .sidebarCardRow(leading: 18, separator: nil)
        .sensoryFeedback(.selection, trigger: showAll)
        .accessibilityLabel(showAll ? "Show fewer" : "Show all \(count) running")
        .accessibilityValue(showAll ? "expanded" : "collapsed")
        .accessibilityIdentifier("nowShowAll")
    }
}

import HerdKit
import SwiftUI

struct SidebarView: View {
    let store: AppStore
    let onSelect: (String) -> Void
    let onNewChat: () -> Void
    let onUnpair: () -> Void

    @State private var query = ""
    @FocusState private var searching: Bool

    var body: some View {
        let sections = store.state.sections(matching: query)
        VStack(spacing: 0) {
            searchField
                .padding(.horizontal, 16)
                .padding(.top, 8)
                .padding(.bottom, 12)

            ScrollView {
                LazyVStack(alignment: .leading, spacing: 2) {
                    SidebarButton(title: "New chat", systemImage: "square.and.pencil", action: onNewChat)
                        .padding(.bottom, 6)

                    ForEach(sections) { section in
                        Text(section.name)
                            .font(.footnote.weight(.semibold))
                            .foregroundStyle(.secondary)
                            .padding(.horizontal, 12)
                            .padding(.top, 16)
                            .padding(.bottom, 4)
                        ForEach(section.agents) { agent in
                            AgentRow(
                                agent: agent,
                                selected: agent.id == store.selectedAgentId,
                                unseen: store.isUnseen(agent)
                            ) {
                                onSelect(agent.id)
                            }
                            .contextMenu {
                                Button(role: .destructive) {
                                    store.interrupt(agent.id)
                                } label: {
                                    Label("Stop", systemImage: "stop.circle")
                                }
                                .disabled(agent.status != .working)
                                Button {} label: {
                                    Label("Rename", systemImage: "pencil")
                                }
                                .disabled(true)
                            } preview: {
                                AgentPreview(agent: agent)
                            }
                        }
                    }

                    if sections.isEmpty {
                        Text(query.isEmpty ? "No agents running" : "No matches")
                            .foregroundStyle(.secondary)
                            .frame(maxWidth: .infinity)
                            .padding(.top, 40)
                    }
                }
                .padding(.horizontal, 8)
                .padding(.bottom, 20)
                .animation(.smooth, value: sections)
            }
            .scrollDismissesKeyboard(.immediately)
            .scrollEdgeEffectStyle(.soft, for: .vertical)

            footer
        }
        .background(Color(.systemBackground))
    }

    private var searchField: some View {
        HStack(spacing: 8) {
            Image(systemName: "magnifyingglass")
                .foregroundStyle(.secondary)
            TextField("Search", text: $query)
                .focused($searching)
                .submitLabel(.search)
            if !query.isEmpty {
                Button {
                    query = ""
                } label: {
                    Image(systemName: "xmark.circle.fill").foregroundStyle(.tertiary)
                }
                .buttonStyle(.plain)
                .accessibilityLabel("Clear search")
            }
        }
        .padding(.horizontal, 14)
        .frame(height: 44)
        .glassEffect(.regular.interactive(), in: .capsule)
    }

    private var footer: some View {
        HStack(spacing: 12) {
            Image(systemName: "desktopcomputer")
                .font(.footnote.weight(.semibold))
                .foregroundStyle(.white)
                .frame(width: 34, height: 34)
                .background(.tint, in: .circle)
            VStack(alignment: .leading, spacing: 1) {
                Text("Herd").font(.subheadline.weight(.semibold))
                Text(store.hostLabel)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
            }
            Spacer()
            Menu {
                Button(role: .destructive, action: onUnpair) {
                    Label("Unpair", systemImage: "link.badge.plus")
                }
            } label: {
                Image(systemName: "ellipsis")
                    .frame(width: 36, height: 36)
                    .contentShape(.rect)
            }
            .tint(.secondary)
        }
        .padding(.horizontal, 20)
        .padding(.vertical, 12)
    }
}

private struct SidebarButton: View {
    let title: String
    let systemImage: String
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            Label(title, systemImage: systemImage)
                .font(.body.weight(.medium))
                .labelStyle(SidebarLabelStyle())
                .padding(.horizontal, 12)
                .padding(.vertical, 11)
                .frame(maxWidth: .infinity, alignment: .leading)
                .contentShape(.rect)
        }
        .buttonStyle(.plain)
    }
}

private struct SidebarLabelStyle: LabelStyle {
    func makeBody(configuration: Configuration) -> some View {
        HStack(spacing: 12) {
            configuration.icon.frame(width: 22)
            configuration.title
        }
    }
}

private struct AgentRow: View {
    let agent: Agent
    let selected: Bool
    let unseen: Bool
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            HStack(spacing: 10) {
                VStack(alignment: .leading, spacing: 3) {
                    Text(agent.displayTitle)
                        .font(.body.weight(unseen || agent.status == .blocked ? .semibold : .regular))
                        .lineLimit(1)
                    Text("\(agent.kind) · \(RelativeTime.short(agent.updatedAt))")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                }
                Spacer(minLength: 4)
                StatusIndicator(status: agent.status, unseen: unseen)
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 9)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(selected ? Color(.secondarySystemFill) : .clear, in: .rect(cornerRadius: 14))
            .contentShape(.rect(cornerRadius: 14))
        }
        .buttonStyle(.plain)
        .accessibilityAddTraits(selected ? .isSelected : [])
    }
}

private struct AgentPreview: View {
    let agent: Agent

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(agent.displayTitle).font(.headline)
            Text("\(agent.workspaceName) · \(agent.kind)").foregroundStyle(.secondary)
            if let name = agent.name {
                Text(name).font(.footnote.monospaced()).foregroundStyle(.tertiary)
            }
        }
        .padding(20)
        .frame(width: 280, alignment: .leading)
    }
}

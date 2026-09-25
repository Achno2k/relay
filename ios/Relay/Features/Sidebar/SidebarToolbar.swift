import RelayKit
import SwiftUI

/// The sidebar's pinned top bar: a glass machine menu on the left, and a glass pill with Filter and ••• on
/// the right. 52 pt tall, 16 pt side padding (design B · Grouped).
struct SidebarToolbar: View {
    @Bindable var store: AppStore
    let onUnpair: () -> Void
    let onUsage: () -> Void

    var body: some View {
        GlassEffectContainer(spacing: 12) {
            HStack(spacing: 12) {
                MachineMenu(store: store)
                    .frame(maxWidth: .infinity, alignment: .leading)
                HStack(spacing: 0) {
                    SidebarFilterMenu(selection: $store.filter, needsInput: store.sidebar.needsInputCount > 0)
                    moreMenu
                }
                .glassEffect(.regular.interactive(), in: .capsule)
            }
        }
        .frame(minHeight: 52)
        .padding(.horizontal, 16)
        // Like the system bars: the toolbar stops growing at AX1 so the machine name and the Filter/••• pill
        // keep their room; long-press shows the large content viewer beyond that.
        .dynamicTypeSize(...DynamicTypeSize.accessibility1)
    }

    private var moreMenu: some View {
        Menu {
            Section {
                Button(action: onUsage) {
                    Label("Usage", systemImage: "gauge")
                }
                .accessibilityIdentifier("sidebarUsage")
            }
            Section {
                Button(role: .destructive, action: onUnpair) {
                    Label("Unpair", systemImage: "link")
                }
            }
        } label: {
            Image(systemName: "ellipsis")
                .font(.body.weight(.semibold))
                .frame(width: 44, height: 44)
                .contentShape(.rect)
        }
        .tint(.primary)
        .accessibilityShowsLargeContentViewer {
            Label("More", systemImage: "ellipsis")
        }
        .accessibilityLabel("More")
        .accessibilityIdentifier("sidebarMore")
    }
}

/// "● LT-MBP-457 ⌄": the paired Mac and its connection. The menu lists the machines the bridge reports.
/// Status is monochrome, so the dot is filled when connected and hollow while reconnecting.
private struct MachineMenu: View {
    let store: AppStore

    private var connected: Bool { store.connection == .connected }
    private var name: String { store.machines.first?.name ?? store.hostLabel }

    var body: some View {
        Menu {
            Section {
                ForEach(store.machines) { machine in
                    Label(machine.name, systemImage: machine.kind == .laptop ? "laptopcomputer" : "desktopcomputer")
                    if let os = machine.os { Label(os, systemImage: "apple.logo") }
                }
                Label(store.hostLabel, systemImage: "network")
            }
            Section {
                Label(connected ? "Connected" : "Reconnecting…",
                      systemImage: connected ? "checkmark.circle" : "wifi.exclamationmark")
            }
        } label: {
            HStack(spacing: 8) {
                // UIColor.label: the menu label dims `.primary` to grey.
                Circle()
                    .strokeBorder(Color(.label), lineWidth: connected ? 0 : 1.5)
                    .background(Circle().fill(connected ? Color(.label) : .clear))
                    .frame(width: 7, height: 7)
                Text(name)
                    .font(.subheadline.weight(.medium))
                    .lineLimit(1)
                    .minimumScaleFactor(0.8)
                Image(systemName: "chevron.down")
                    .font(.caption.weight(.bold))
                    .foregroundStyle(.secondary)
            }
            .padding(.leading, 16)
            .padding(.trailing, 14)
            .frame(minHeight: 44)
            .contentShape(.capsule)
        }
        .tint(.primary)
        .glassEffect(.regular.interactive(), in: .capsule)
        .accessibilityShowsLargeContentViewer {
            Label(name, systemImage: "desktopcomputer")
        }
        .accessibilityLabel("Machine: \(name)")
        .accessibilityValue(connected ? "Connected" : "Reconnecting")
        .accessibilityIdentifier("sidebarMachineMenu")
    }
}

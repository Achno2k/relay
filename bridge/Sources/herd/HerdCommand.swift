import ArgumentParser
import Foundation
import HerdCore

@main
struct HerdCommand: AsyncParsableCommand {
    static let configuration = CommandConfiguration(
        commandName: "herd",
        abstract: "Bridge between herdr and the Herd iOS app.",
        version: Herd.version,
        subcommands: [Serve.self, Pair.self, Token.self, InstallLaunchd.self],
        defaultSubcommand: Serve.self)
}

struct Serve: AsyncParsableCommand {
    static let configuration = CommandConfiguration(abstract: "Run the HTTP + WebSocket bridge.")

    @Option(help: "Port to listen on.") var port = 7878
    @Flag(help: "Bind to 127.0.0.1 only, even if Tailscale is up.") var localOnly = false

    func run() async throws {
        let token = try TokenStore.load()
        var hosts = ["127.0.0.1"]
        if !localOnly {
            if let ip = Tailscale.ipv4() {
                hosts.append(ip)
            } else {
                print("tailscale ip -4 unavailable; listening on 127.0.0.1 only")
            }
        }
        try await HerdApp(port: port, hosts: hosts, token: token).run()
    }
}

struct Pair: ParsableCommand {
    static let configuration = CommandConfiguration(abstract: "Print the pairing QR code and URL.")

    @Option(help: "Port the bridge listens on.") var port = 7878
    @Option(help: "Host to put in the URL (defaults to the Tailscale IPv4).") var host: String?

    func run() throws {
        let token = try TokenStore.load()
        let resolved = host ?? Tailscale.ipv4()
        if resolved == nil {
            print("warning: no Tailscale IPv4 found; using 127.0.0.1 (only reachable from this Mac)\n")
        }
        let url = Pairing.url(host: resolved ?? "127.0.0.1", port: port, token: token)
        if let qr = QRCode.terminal(url) { print(qr) }
        print(url)
    }
}

struct Token: ParsableCommand {
    static let configuration = CommandConfiguration(abstract: "Print the bearer token.")

    @Flag(help: "Replace the token. Paired phones must pair again.") var rotate = false

    func run() throws {
        print(rotate ? try TokenStore.rotate() : try TokenStore.load())
        if rotate { FileHandle.standardError.write(Data("rotated; restart `herd serve` and re-run `herd pair`\n".utf8)) }
    }
}

struct InstallLaunchd: ParsableCommand {
    static let configuration = CommandConfiguration(
        commandName: "install-launchd",
        abstract: "Write a LaunchAgent plist for `herd serve` (does not load it).")

    @Option(help: "Port to serve on.") var port = 7878

    func run() throws {
        let exe = URL(fileURLWithPath: CommandLine.arguments[0]).standardizedFileURL.resolvingSymlinksInPath().path
        let data = try LaunchAgent.plist(executable: exe, port: port)
        let url = LaunchAgent.plistURL
        try FileManager.default.createDirectory(at: url.deletingLastPathComponent(), withIntermediateDirectories: true)
        try data.write(to: url)
        print("wrote \(url.path)")
        print("load it with:\n  launchctl bootstrap gui/$(id -u) \(url.path)")
        print("unload with:\n  launchctl bootout gui/$(id -u)/\(LaunchAgent.label)")
    }
}

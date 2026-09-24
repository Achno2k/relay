import Foundation
import Hummingbird
import HummingbirdWebSocket
import Logging
import ServiceLifecycle
import Synchronization

/// Wires herdr, the monitor and one HTTP server per bind address.
public struct HerdApp: Sendable {
    public var port: Int
    public var hosts: [String]
    public var token: String
    public var socketPath: String

    public init(port: Int, hosts: [String], token: String, socketPath: String = HerdrClient.defaultSocketPath) {
        self.port = port
        self.hosts = hosts
        self.token = token
        self.socketPath = socketPath
    }

    public func run() async throws {
        let logger: Logger = {
            var l = Logger(label: "herd")
            l.logLevel = .info
            return l
        }()
        let port = self.port
        let herdr = HerdrClient(socketPath: socketPath)
        let monitorRef = Mutex<AgentMonitor?>(nil)
        let service = AgentService(herdr: herdr, locator: TranscriptLocator(), onChange: {
            guard let m = monitorRef.withLock({ $0 }) else { return }
            Task { await m.trigger() }
        })
        let hub = EventHub()
        let monitor = AgentMonitor(service: service, hub: hub, stream: HerdrEventStream(socketPath: socketPath))
        monitorRef.withLock { $0 = monitor }
        let router = HerdRoutes.router(service: service, hub: hub, token: token)

        var services: [any Service] = [monitor, UploadCleaner(store: service.uploads, logger: logger)]
        for host in hosts {
            let app = Application(
                router: router,
                server: .http1WebSocketUpgrade(webSocketRouter: router),
                configuration: .init(address: .hostname(host, port: port), serverName: "herd"),
                onServerRunning: { _ in logger.info("listening on http://\(host):\(port)") },
                logger: logger)
            services.append(app)
        }
        let group = ServiceGroup(configuration: .init(services: services, gracefulShutdownSignals: [.sigterm, .sigint], logger: logger))
        try await group.run()
    }
}

import Foundation
import HerdCore
print(Timestamps.format(.now), Timestamps.normalize("2026-09-23T13:16:54.975Z") ?? "nil", Timestamps.normalize("2026-09-23T13:16:54Z") ?? "nil", Timestamps.normalize(NSNumber(value: 1790000000000)) ?? "nil")

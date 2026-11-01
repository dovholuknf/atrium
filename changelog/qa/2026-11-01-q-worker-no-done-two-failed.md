# Worker with no done saw two failed events (test race)

TestAWorkerThatEndsWithNoDoneFailsTheTaskAndKeepsTheFolder failed once on CI with `events: [failed failed]`.
The room resends an owed task event by design, and the hub takes a replay as nothing. The flush that createItem
kicks in the background can land after the test's own send and resend the owed `failed`. The test now treats a
repeated `failed` as one. No product code changed.

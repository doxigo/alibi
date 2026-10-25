package main

// Windows' "Set time zone automatically" moves the zone to where Windows' location service puts the
// computer, which is your real location, so while it's on the agent leaves the zone alone. It's a
// machine-wide setting that takes an administrator to change, so the companion leaves it to you: it
// opens Settings > Time & language > Date & time, where you switch it off, and back on whenever you
// like. Nothing is changed behind your back, and nothing needs undoing at uninstall.

const dateTimeSettings = "ms-settings:dateandtime"

func openDateTimeSettings() { shellOpen(dateTimeSettings) }

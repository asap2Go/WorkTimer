# Work Time Counter
[![Go](https://github.com/asap2Go/WorkTimer/actions/workflows/build.yml/badge.svg)](https://github.com/asap2Go/WorkTimer/actions/workflows/build.yml)

Work Time Counter counts the time the user is active.

![main](https://github.com/asap2Go/Work-Time-Counter/assets/96501510/a801a5e1-556c-4c9c-ab11-0f3a0ccb8be7)

The counter will pause if the windows lock screen is active 

![stopped](https://github.com/asap2Go/Work-Time-Counter/assets/96501510/527016f7-4d4d-4995-8bb5-9a96caf81379)

or the last user input is older than a specifiable timeout (by default 6 minutes).

![timeout](https://github.com/asap2Go/Work-Time-Counter/assets/96501510/632d713f-4658-44e2-b3e5-e77852e3e3d1)

The counter can also be stopped and restarted manually.
After every stop or start the end and start times will be shown in the app window.

![main_stop](https://github.com/asap2Go/Work-Time-Counter/assets/96501510/6701d267-f582-4461-a4ae-ecbafaaceb05)

The time worked can be copied in hh:mm format.

The app minimizes 2 seconds after startup into a system tray as not to clutter the desktop.

![tray](https://github.com/asap2Go/Work-Time-Counter/assets/96501510/1b4c8cb9-c86a-41a6-92f2-fab6be86f86b)

but can be recalled anytime with the show option of the system tray

![tray_options](https://github.com/asap2Go/Work-Time-Counter/assets/96501510/e21146a0-ca5e-4d12-8255-31aa37a73674)

It is written entirely in Go and licensed under the MIT license.

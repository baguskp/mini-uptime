# MiniUptime Domain Glossary

## Display

Read-only operational view for showing the current state of the selected monitors.

## Online monitor

A monitor whose latest check status is `up`. A monitor with no completed check or a latest status of `down` is not online.

## Display scope

The set of enabled monitors rendered on `/display`. Without a group filter it contains all enabled monitors; with `?group=...`, it contains only enabled monitors in that group.

## Display summary

The header metrics for the current display scope: online count, total count, and online percentage. The summary is scoped to the monitors currently visible on the display.

## Online rate

The percentage of monitors in the display scope whose latest check status is `up`.

## Average latency

The arithmetic mean latency of the last 50 completed checks for one monitor. If no checks exist, the display shows `—`.

## Last checked

The local time of the most recent completed check for a monitor. If no completed check exists, the display shows `Never checked`.

## Group

A named collection used to organize monitors for administration and display filtering.

## Monitor assignment

The relationship between a monitor and a group. A monitor belongs to at most one group; assigning it to another group moves it from its previous group.

## Group deletion

Removing a group clears the grouping relationship but preserves its monitors, checks, and incidents.

# Specifications of expiry-radar / extensions/vscode

What this code does, one atomic Gherkin scenario per behaviour, read from the code by code-atlas.
Tags: `@verified` a test asserts it · `@inferred` read from the code only · `@gap` an entry point no test covers · `@human` written or amended by a person, never rewritten.

## Config file insights

Linking expiring items back to the lines in expiry-radar.json that declared them.

- **Declaration lookup**: [Locating where tracked items are declared in the config](config-insights/declaration-lookup/config-declaration-lookup.feature) (6), [Pointing recorded items at their config line](config-insights/declaration-lookup/recorded-vs-discovered-placement.feature) (1)
- **Expiry squiggles**: [Flagging expiring items in the config file](config-insights/expiry-squiggles/config-expiry-squiggles.feature) (3)

## Inventory panel

The tree view and status bar where operators browse what is expiring, in blast-radius order.

- **Attention indicators**: [Signalling what needs action today](inventory-panel/attention-indicators/inventory-attention-badge.feature) (2), [Status bar summary of what expires next](inventory-panel/attention-indicators/status-bar-next-deadline.feature) (7)
- **Filtering**: [Filtering the inventory](inventory-panel/filtering/inventory-filtering.feature) (3)
- **Grouping and expansion**: [Grouping the inventory by kind](inventory-panel/grouping/inventory-grouping.feature) (2)
- **Item actions**: [Copying an item's details](inventory-panel/item-actions/copy-item-details.feature) (1), [Acting on an inventory item](inventory-panel/item-actions/inventory-item-actions.feature) (1)
- **Ranked list**: [Seeing what expires in priority order](inventory-panel/ranked-list/inventory-ranked-list.feature) (6)

## Reports and probes

One-off documents and answers produced outside the regular inventory refresh.

- **Host probe**: [Naming the host to probe](reports-and-probes/host-probe/host-probe-prompt.feature) (1), [Probing a single host](reports-and-probes/host-probe/host-probe.feature) (2), [Probing a single host](reports-and-probes/host-probe/one-off-probe.feature) (1)
- **HTML report view**: [Viewing the HTML report](reports-and-probes/html-report/html-report-freshness.feature) (1), [Viewing the HTML report in the editor](reports-and-probes/html-report/html-report-view.feature) (4)
- **Report export**: [Choosing an export format](reports-and-probes/report-export/export-format-choice.feature) (1), [Exporting the report to a file](reports-and-probes/report-export/report-export.feature) (1)

## Scanning

Collecting the ranked expiry inventory by running expiry-radar and turning its results into what the editor shows.

- **Automatic refresh**: [Keeping the inventory fresh automatically](scanning/automatic-refresh/automatic-refresh.feature) (5), [Pacing automatic scans](scanning/automatic-refresh/pacing-automatic-scans.feature) (4)
- **Failed-source reporting**: [Detecting failed sources](scanning/failed-sources/failed-source-detection.feature) (1)
- **Inventory snapshot**: [Keeping one current inventory per workspace folder](scanning/inventory-snapshot/inventory-snapshot-per-folder.feature) (2), [Reading the inventory report](scanning/inventory-snapshot/inventory-snapshot-reading.feature) (6)
- **On-demand scan**: [Controlling scans in progress](scanning/on-demand-scan/controlling-scans.feature) (1), [Scanning the estate on demand](scanning/on-demand-scan/on-demand-scan.feature) (3), [Running a collection](scanning/on-demand-scan/running-a-collection.feature) (6)

## Setup and troubleshooting

Getting the extension connected to the CLI and finding out why a scan came back empty or wrong.

- **Extension activation**: [Extension activation](setup-and-troubleshooting/activation/extension-activation.feature) (1)
- **Activity log**: [Reading the extension's activity log](setup-and-troubleshooting/activity-log/activity-log.feature) (1)
- **CLI discovery and install**: [Finding the expiry-radar CLI](setup-and-troubleshooting/cli-discovery/cli-discovery.feature) (2)
- **Environment check**: [Diagnosing an empty or short inventory](setup-and-troubleshooting/environment-check/environment-doctor.feature) (7)
- **Settings**: [Shaping a collection from settings](setup-and-troubleshooting/settings/collection-settings.feature) (4), [Keeping extension settings within safe limits](setup-and-troubleshooting/settings/settings-sanity-limits.feature) (2)
- **Workspace folders**: [Choosing the workspace folder a command applies to](setup-and-troubleshooting/workspace-folders/command-target-folder.feature) (2)

## Tracking items

Recording and removing things to watch without hand-editing the config schema.

- **Add item**: [Recording a new item from the editor](tracking-items/add-item/add-tracked-item.feature) (2), [Recording an item to track](tracking-items/add-item/recording-an-item.feature) (5)
- **Config file writing**: [Writing tracked items into the config file](tracking-items/config-writing/config-file-writing.feature) (6), [Creating a starter config file](tracking-items/config-writing/starter-config.feature) (1)
- **Entry validation**: [Recording items in the config](tracking-items/entry-validation/recording-entries.feature) (4)
- **Remove item**: [Stopping tracking of a recorded item](tracking-items/remove-item/remove-tracked-item.feature) (4)

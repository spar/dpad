#!/bin/sh
# Stands in for the user's editor in the TUI scenario: it makes a small change
# to the Compose file copy so the edit review has a diff to show. The scenario
# discards the change.
sed -i -e 's/nginx:1.27-alpine/nginx:1.28-alpine/' -e 's/mem_limit: 64m/mem_limit: 96m/' "$1"

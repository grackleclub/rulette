#!/usr/bin/env bash

htmx_url="https://cdn.jsdelivr.net/npm/htmx.org@2.0.6/dist/htmx.min.js"
js_dir="./static/js"
htmx_path="$js_dir/htmx.min.js"

if ! curl --create-dirs -so "$htmx_path" "$htmx_url"; then
	echo "error: couldn not update htmx.js"
	exit 1
fi
echo "success: updated htmx vendoring to $htmx_path"


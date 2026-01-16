#!/usr/bin/env python3
"""Convert a device profile YAML's deviceResources into xcj910.json-like structure.

Usage:
  python3 totsl.py [--input profile.yml] [--output output.json]

If --output is not provided the script will write a file named
"<top-level-name>.json" next to the input YAML file. Chinese characters are
preserved in the output (UTF-8).
"""

from __future__ import annotations

import argparse
import json
import os
import sys

try:
	import yaml
except Exception as e:  # pragma: no cover - runtime dependency
	print("Missing dependency: PyYAML is required. Install with: pip install pyyaml", file=sys.stderr)
	raise


TYPE_MAP = {
	"Int16": "int",
	"Int32": "int",
	"Uint16": "int",
	"Uint32": "int",
	"Uint8": "int",
	"Float32": "float",
	"Float64": "double",
	"Bool": "boolean",
}


def convert_resource(res: dict) -> dict:
	"""Convert a single deviceResource dict to the target JSON property format."""
	rid = res.get("name")
	rname = res.get("description") or rid

	# valueType may be under properties.valueType as a scalar string
	props = res.get("properties", {})
	vt = props.get("valueType")
	# If valueType is a mapping (unlikely in current yml), try to extract a value
	if isinstance(vt, dict):
		# Example: { type: Int32 } or similar; prefer 'type' key
		vt_val = vt.get("type") or next(iter(vt.values()), None)
	else:
		vt_val = vt

	mapped = TYPE_MAP.get(vt_val, None)
	if mapped is None and isinstance(vt_val, str):
		# fallback: normalize to lowercase string
		mapped = vt_val.lower()
	elif mapped is None:
		mapped = "string"

	return {
		"id": rid,
		"name": rname,
		"expands": {
			"source": "device",
			"type": ["report"],
			"groupId": "group_1",
			"groupName": "分组_1",
		},
		"valueType": {"type": mapped},
	}


def convert(yaml_path: str) -> dict:
	with open(yaml_path, "r", encoding="utf-8") as f:
		data = yaml.safe_load(f)

	top_name = data.get("name") if isinstance(data, dict) else None
	resources = data.get("deviceResources", []) if isinstance(data, dict) else []

	properties = [convert_resource(r) for r in resources]

	return {"properties": properties}, top_name


def main(argv: list[str] | None = None) -> int:
	p = argparse.ArgumentParser(description="Convert profile.yml deviceResources to JSON properties")
	p.add_argument("--input", "-i", default="profile.yml", help="input YAML file path")
	p.add_argument("--output", "-o", default=None, help="output JSON file path")
	args = p.parse_args(argv)

	json_obj, top_name = convert(args.input)

	out_path = args.output
	if out_path is None:
		if not top_name:
			print("YAML top-level 'name' missing; provide --output explicitly", file=sys.stderr)
			return 2
		safe_name = top_name.replace(" ", "_")
		out_path = os.path.join(os.path.dirname(args.input) or ".", f"{safe_name}.json")

	with open(out_path, "w", encoding="utf-8") as f:
		json.dump(json_obj, f, ensure_ascii=False, indent=4)

	print(f"Wrote {out_path}")
	return 0


if __name__ == "__main__":
	raise SystemExit(main())


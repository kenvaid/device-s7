#!/usr/bin/env python3
"""
genprofile.py

Generate a device profile YAML from a template YAML and an Excel file.

Usage:
  python genprofile.py template.yml data.xlsx -o out.yml

Requirements:
  pip install pyyaml openpyxl

Rules implemented:
  - Each Excel row -> one deviceResource
    description <- column A
    NodeName    <- column B
    valueType   <- column C
    name        <- z-base-32 encoding of NodeName
  - deviceCommands: names placed into "Alarms" if valueType is Bool (case-insensitive), otherwise into "Values"
  - Other parts of the template are preserved

The script will try to reuse field defaults from the template (e.g. isHidden, readWrite) when available.
"""
from __future__ import annotations

import argparse
import copy
import sys
from typing import List, Dict, Any, Tuple

import openpyxl
import yaml


ZBASE32_ALPHABET = "ybndrfg8ejkmcpqxot1uwisza345h769"


def zbase32_encode(data: bytes) -> str:
    """Encode bytes into z-base-32 without padding.

    Implementation: consume bits MSB-first and emit 5-bit groups mapping to the z-base-32 alphabet.
    """
    if not data:
        return ""
    alphabet = ZBASE32_ALPHABET
    value = 0
    bits = 0
    out_chars: List[str] = []
    for b in data:
        value = (value << 8) | b
        bits += 8
        while bits >= 5:
            shift = bits - 5
            index = (value >> shift) & 0x1F
            out_chars.append(alphabet[index])
            bits -= 5
            value &= (1 << shift) - 1 if shift > 0 else 0
    if bits > 0:
        # pad the last partial group with zeros on the right
        index = (value << (5 - bits)) & 0x1F
        out_chars.append(alphabet[index])
    return "".join(out_chars)


def read_excel_rows(xlsx_path: str) -> List[Tuple[str, str, str]]:
    """Read rows from the first sheet of the Excel workbook.

    Returns a list of tuples (description, NodeName, valueType).
    Automatically skips an initial header row if it looks like a header.
    """
    wb = openpyxl.load_workbook(xlsx_path, read_only=True, data_only=True)
    ws = wb[wb.sheetnames[0]]
    rows = []
    iter_rows = list(ws.iter_rows(values_only=True))
    if not iter_rows:
        return rows
    # detect header: if first row contains known header names
    first = iter_rows[0]
    header_like = False
    if any(cell is not None and isinstance(cell, str) and cell.strip().lower() in ("description", "nodename", "valueType".lower()) for cell in first):
        header_like = True
    # also detect common headers words
    if any(cell is not None and isinstance(cell, str) and any(k in cell.strip().lower() for k in ("description", "node", "name", "value")) for cell in first):
        header_like = True

    start = 1 if header_like else 0
    for r in iter_rows[start:]:
        # Excel columns A,B,C are indices 0,1,2
        desc = r[0] if len(r) >= 1 else None
        nodename = r[1] if len(r) >= 2 else None
        valtype = r[2] if len(r) >= 3 else None
        if desc is None and nodename is None and valtype is None:
            continue
        # convert to strings
        desc_s = str(desc).strip() if desc is not None else ""
        nodename_s = str(nodename).strip() if nodename is not None else ""
        valtype_s = str(valtype).strip() if valtype is not None else ""
        rows.append((desc_s, nodename_s, valtype_s))
    return rows


def build_device_resources(rows: List[Tuple[str, str, str]], proto: Dict[str, Any] | None) -> List[Dict[str, Any]]:
    res = []
    for desc, nodename, valtype in rows:
        if not nodename:
            # skip rows without NodeName
            continue
        name_z = zbase32_encode(nodename.encode("utf-8"))
        if proto:
            entry = copy.deepcopy(proto)
            entry["name"] = name_z
            entry["description"] = desc
            # set properties.valueType
            if "properties" not in entry or entry["properties"] is None:
                entry["properties"] = {}
            entry["properties"]["valueType"] = valtype
            # ensure attributes.NodeName points to original nodename
            if "attributes" not in entry or entry["attributes"] is None:
                entry["attributes"] = {}
            entry["attributes"]["NodeName"] = nodename
        else:
            entry = {
                "name": name_z,
                "description": desc,
                "properties": {"valueType": valtype, "readWrite": "R"},
                "attributes": {"NodeName": nodename},
            }
        res.append(entry)
    return res


def build_device_commands(rows: List[Tuple[str, str, str]], resources: List[Dict[str, Any]], proto_commands: List[Dict[str, Any]] | None) -> List[Dict[str, Any]]:
    # Map from NodeName (original) to encoded name
    nodename_to_name = {}
    for (desc, nodename, valtype), r in zip(rows, resources):
        nodename_to_name[nodename] = r.get("name")

    values_ops = []
    alarms_ops = []
    for (desc, nodename, valtype) in rows:
        if not nodename:
            continue
        name_z = nodename_to_name.get(nodename) or zbase32_encode(nodename.encode("utf-8"))
        op = {"deviceResource": name_z}
        if valtype.strip().lower() == "bool":
            alarms_ops.append(op)
        else:
            values_ops.append(op)

    # Find prototypes for Values and Alarms in proto_commands if provided
    def find_proto(name: str) -> Dict[str, Any] | None:
        if not proto_commands:
            return None
        for c in proto_commands:
            if c.get("name") == name:
                return c
        return None

    values_proto = find_proto("Values")
    alarms_proto = find_proto("Alarms")

    commands: List[Dict[str, Any]] = []

    if values_proto is not None:
        c = copy.deepcopy(values_proto)
        c["resourceOperations"] = values_ops
        commands.append(c)
    else:
        commands.append({"name": "Values", "readWrite": "R", "isHidden": False, "resourceOperations": values_ops})

    if alarms_proto is not None:
        c = copy.deepcopy(alarms_proto)
        c["resourceOperations"] = alarms_ops
        commands.append(c)
    else:
        commands.append({"name": "Alarms", "readWrite": "R", "isHidden": False, "resourceOperations": alarms_ops})

    # Keep any other commands from prototype that are not Values/Alarms
    if proto_commands:
        for c in proto_commands:
            if c.get("name") not in ("Values", "Alarms"):
                commands.append(copy.deepcopy(c))

    return commands


def main(argv: List[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description="Generate profile YAML from template and Excel")
    parser.add_argument("-t", "--template", help="Path to template YAML file")
    parser.add_argument("-e", "--excel", help="Path to Excel file (.xlsx)")
    parser.add_argument("-o", "--out", help="Output YAML file path", default="generated.profile.yml")
    args = parser.parse_args(argv)

    # Load template
    with open(args.template, "r", encoding="utf-8") as f:
        template = yaml.safe_load(f)

    # Read excel rows
    rows = read_excel_rows(args.excel)
    if not rows:
        print("No rows found in Excel file.", file=sys.stderr)
        return 2

    # Determine prototype deviceResource if present
    proto_dr = None
    if isinstance(template, dict) and "deviceResources" in template and isinstance(template["deviceResources"], list) and len(template["deviceResources"])>0:
        proto_dr = copy.deepcopy(template["deviceResources"][0])

    device_resources = build_device_resources(rows, proto_dr)

    # Determine prototype deviceCommands if present
    proto_cmds = None
    if isinstance(template, dict) and "deviceCommands" in template and isinstance(template["deviceCommands"], list):
        proto_cmds = template["deviceCommands"]

    device_commands = build_device_commands(rows, device_resources, proto_cmds)

    # Update template with new lists, leaving other fields intact
    out = copy.deepcopy(template)
    out["deviceResources"] = device_resources
    out["deviceCommands"] = device_commands

    # Write output
    with open(args.out, "w", encoding="utf-8") as f:
        yaml.safe_dump(out, f, default_flow_style=False, sort_keys=False, allow_unicode=True)

    print(f"Wrote generated profile to: {args.out}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

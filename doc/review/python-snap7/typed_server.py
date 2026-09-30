"""Explicit test adapter for missing typed I/O in python-snap7 3.2.0.

This does NOT patch the installed package and is not an upstream compatibility
claim. Upstream retains TPKT/COTP/session handling and registered-area storage.
"""

import struct

from s7.server import Server


class TypedServer(Server):
    WIDTHS = {1: 1, 2: 1, 3: 1, 4: 2, 5: 2, 6: 4, 7: 4, 8: 4, 0x1C: 2, 0x1D: 2}

    def _parse_address_specification(self, spec):
        if len(spec) != 12 or spec[:3] != b"\x12\x0a\x10":
            return {}
        result = super()._parse_address_specification(spec)
        if result:
            result["bit"] = int.from_bytes(spec[9:12], "big") % 8
        return result

    def _parse_request_parameters(self, params):
        if params and params[0] == 5:
            if len(params) < 2 or not 1 <= params[1] <= 20 or len(params) != 2 + params[1] * 12:
                return {"function_code": 5, "item_count": 0}
            specs = [self._parse_address_specification(params[i:i + 12])
                     for i in range(2, len(params), 12)]
            return {"function_code": 5, "item_count": params[1],
                    "address_specs": specs, "address_spec": specs[0]}
        return super()._parse_request_parameters(params)

    def _parse_data_section(self, data):
        result = super()._parse_data_section(data)
        result["raw_items"] = bytes(data)
        return result

    @staticmethod
    def _transport(word, size, count):
        if word == 1:
            return 3, count
        if word in (3, 0x1C, 0x1D):
            return 9, size
        if word == 8:
            return 7, size
        if word in (5, 7):
            return 5, size * 8
        return 4, size * 8

    @staticmethod
    def _ack(request, function, count, data):
        header = struct.pack(">BBHHHHBB", 0x32, 3, 0, request["sequence"], 2, len(data), 0, 0)
        return header + bytes((function, count)) + data

    def _valid_spec(self, spec):
        if not spec or spec.get("word_len") not in self.WIDTHS or spec.get("count", 0) <= 0:
            return False
        word, area = spec["word_len"], int(spec["area"])
        if (area == 0x1C) != (word == 0x1C) or (area == 0x1D) != (word == 0x1D):
            return False
        return word != 1 or spec["count"] == 1

    def _handle_read_area(self, request, client_address):
        params = request.get("parameters", {})
        specs = params.get("address_specs") or [params.get("address_spec", {})]
        if not 1 <= len(specs) <= 20 or len(specs) != params.get("item_count"):
            return self._build_error_response(request, 0x8001)
        result = bytearray()
        for i, spec in enumerate(specs):
            if not self._valid_spec(spec):
                result.extend(b"\x05\x00\x00\x00")
                continue
            word, count = spec["word_len"], spec["count"]
            size = count * self.WIDTHS[word]
            status, data = self._read_from_memory_area(spec["area"], spec["db_number"], spec["start"], size)
            if status != 0xFF:
                result.extend(bytes((status, 0, 0, 0)))
                continue
            if word == 1:
                data = bytes(((data[0] >> spec["bit"]) & 1,))
            transport, length = self._transport(word, size, count)
            result.extend(struct.pack(">BBH", 0xFF, transport, length))
            result.extend(data)
            if i < len(specs) - 1 and size % 2:
                result.append(0)
        return self._ack(request, 4, len(specs), bytes(result))

    def _handle_write_area(self, request, client_address):
        params = request.get("parameters", {})
        specs = params.get("address_specs", [])
        if not 1 <= len(specs) <= 20 or len(specs) != params.get("item_count"):
            return self._build_error_response(request, 0x8001)
        raw = request.get("data", {}).get("raw_items", b"")
        operations, offset = [], 0
        # Parse the complete packet before applying any memory changes.
        for i, spec in enumerate(specs):
            if len(raw) - offset < 4:
                return self._build_error_response(request, 0x8001)
            reserved, transport, length = struct.unpack(">BBH", raw[offset:offset + 4])
            offset += 4
            if transport in (4, 5):
                if length % 8:
                    return self._build_error_response(request, 0x8001)
                size = length // 8
            elif transport == 3:
                size = (length + 7) // 8
            elif transport in (6, 7, 9):
                size = length
            else:
                return self._build_error_response(request, 0x8001)
            if reserved != 0 or size > len(raw) - offset:
                return self._build_error_response(request, 0x8001)
            operations.append((spec, transport, length, raw[offset:offset + size]))
            offset += size
            if i < len(specs) - 1 and size % 2:
                if offset >= len(raw):
                    return self._build_error_response(request, 0x8001)
                offset += 1
        if offset != len(raw):
            return self._build_error_response(request, 0x8001)
        statuses = bytearray()
        for spec, transport, length, data in operations:
            if not self._valid_spec(spec):
                statuses.append(5)
                continue
            word, count = spec["word_len"], spec["count"]
            expected = self._transport(word, count * self.WIDTHS[word], count)
            if (transport, length) != expected or (word == 1 and data[0] not in (0, 1)):
                statuses.append(7)
                continue
            key = (spec["area"], spec["db_number"])
            if key not in self.memory_areas:
                statuses.append(0x0A)
                continue
            start = spec["start"]
            with self.area_locks[key]:
                memory = self.memory_areas[key]
                if start < 0 or start + len(data) > len(memory):
                    statuses.append(5)
                    continue
                if word == 1:
                    mask = 1 << spec["bit"]
                    memory[start] = (memory[start] & (~mask & 0xFF)) | (data[0] << spec["bit"])
                else:
                    memory[start:start + len(data)] = data
            statuses.append(0xFF)
        return self._ack(request, 5, len(specs), bytes(statuses))

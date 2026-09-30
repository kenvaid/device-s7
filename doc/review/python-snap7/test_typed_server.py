"""Fixed wire vectors; no gos7 encoders are involved in these checks."""
import unittest

from s7.type import SrvArea
from typed_server import TypedServer


class WireVectors(unittest.TestCase):
    def setUp(self):
        self.server = TypedServer(log=False)
        self.memory = {}
        for name, area in (("M", SrvArea.MK), ("I", SrvArea.PE), ("Q", SrvArea.PA), ("CT", SrvArea.CT)):
            data = bytearray([0xAA] * 64)
            self.server.register_area(area, 0, data)
            self.memory[name] = data

    def exchange(self, wire):
        return self.server._process_request(bytes.fromhex(wire), ("127.0.0.1", 1))

    def test_counter_octet_length_and_element_offset(self):
        self.memory["CT"][14:16] = b"\x01\x23"
        reply = self.exchange("32 01 0000 1234 000e 0000 04 01 12 0a 10 1c 0001 0000 1c 000007")
        self.assertEqual(reply, bytes.fromhex("32 03 0000 1234 0002 0006 0000 04 01 ff 09 0002 0123"))

    def test_bit_preserves_other_seven_bits(self):
        reply = self.exchange("32 01 0000 1234 000e 0005 05 01 12 0a 10 01 0001 0000 83 00008f 00 03 0001 00")
        self.assertEqual(reply, bytes.fromhex("32 03 0000 1234 0002 0001 0000 05 01 ff"))
        expected = bytearray([0xAA] * 64)
        expected[17] = 0x2A
        self.assertEqual(self.memory["M"], expected)

    def test_char_real_then_byte_write(self):
        reply = self.exchange(
            "32 01 0000 1234 0026 0015 05 03 "
            "12 0a 10 03 0003 0000 81 000018 "
            "12 0a 10 08 0001 0000 82 000028 "
            "12 0a 10 02 0001 0000 83 000058 "
            "00 09 0003 533721 00 00 07 0004 3fa00000 00 04 0008 5a"
        )
        self.assertEqual(reply, bytes.fromhex("32 03 0000 1234 0002 0003 0000 05 03 ffffff"))
        self.assertEqual(self.memory["I"][3:6], b"S7!")
        self.assertEqual(self.memory["Q"][5:9], bytes.fromhex("3fa00000"))
        self.assertEqual(self.memory["M"][11], 0x5A)

    def test_out_of_range_does_not_truncate_write(self):
        reply = self.exchange("32 01 0000 1234 000e 0007 05 01 12 0a 10 02 0003 0000 83 0001f8 00 04 0018 112233")
        self.assertEqual(reply, bytes.fromhex("32 03 0000 1234 0002 0001 0000 05 01 05"))
        self.assertEqual(self.memory["M"], bytearray([0xAA] * 64))


if __name__ == "__main__":
    unittest.main(verbosity=2)

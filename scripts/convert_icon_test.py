#!/usr/bin/env python3
"""Regression tests for scripts/convert-icon.py.

Loaded via importlib (not a plain import) because the hyphen in
"convert-icon.py" isn't a valid Python identifier. Run with:

    python3 scripts/convert_icon_test.py
"""
import importlib.util
import os
import subprocess
import sys
import tempfile
import unittest

from PIL import Image

_SCRIPT_PATH = os.path.join(os.path.dirname(__file__), "convert-icon.py")


def _load_convert_icon():
    spec = importlib.util.spec_from_file_location("convert_icon", _SCRIPT_PATH)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


convert_icon = _load_convert_icon()


class ConvertTests(unittest.TestCase):
    # Known alpha values at distinct pixels; convert() must map each source
    # alpha to an opaque grayscale pixel (a, a, a, 255) — including alpha=0,
    # which must become opaque black rather than staying transparent.
    ALPHA_PIXELS = {
        (0, 0): 0,
        (1, 0): 128,
        (0, 1): 255,
    }

    def _make_source(self, path: str) -> None:
        im = Image.new("RGBA", (2, 2), (200, 100, 50, 255))
        px = im.load()
        for (x, y), a in self.ALPHA_PIXELS.items():
            # Color channels are deliberately non-grayscale so the test
            # would fail if convert() read color instead of alpha.
            px[x, y] = (200, 100, 50, a)
        im.save(path)

    def test_alpha_becomes_opaque_grayscale(self):
        with tempfile.TemporaryDirectory() as tmp:
            src = os.path.join(tmp, "src.png")
            dst = os.path.join(tmp, "dst.png")
            self._make_source(src)

            convert_icon.convert(src, dst)

            with Image.open(dst) as out:
                out = out.convert("RGBA")
                opx = out.load()
                for (x, y), a in self.ALPHA_PIXELS.items():
                    self.assertEqual(
                        opx[x, y], (a, a, a, 255),
                        f"pixel {(x, y)} = {opx[x, y]}, want {(a, a, a, 255)}",
                    )

    def test_in_place_conversion_round_trips(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = os.path.join(tmp, "icon.png")
            self._make_source(path)

            convert_icon.convert(path, path)

            with Image.open(path) as out:
                out = out.convert("RGBA")
                opx = out.load()
                for (x, y), a in self.ALPHA_PIXELS.items():
                    self.assertEqual(opx[x, y], (a, a, a, 255))

    def test_main_exits_nonzero_on_wrong_argc(self):
        result = subprocess.run(
            [sys.executable, _SCRIPT_PATH],
            capture_output=True,
            text=True,
        )
        self.assertNotEqual(result.returncode, 0)


if __name__ == "__main__":
    unittest.main()

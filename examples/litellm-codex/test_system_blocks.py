import copy
import unittest

from system_blocks import normalize_system_blocks


class SystemBlocksTest(unittest.TestCase):
    def test_preserves_all_instructions_order_and_unrelated_fields(self):
        data = {
            "model": "subscription-coder",
            "system": [
                {"type": "text", "text": "First\nline", "cache_control": {"type": "ephemeral"}},
                {"type": "text", "text": "Second instruction: λ"},
            ],
            "messages": [{"role": "user", "content": "Create the file"}],
            "tools": [{"name": "write", "input_schema": {"type": "object"}}],
            "stream": True,
            "max_tokens": 256,
        }
        before = copy.deepcopy(data)
        result = normalize_system_blocks(data, {"subscription-coder"})
        self.assertEqual(result["system"], "First\nline\n\nSecond instruction: λ")
        self.assertEqual(data, before)
        for key in ("model", "messages", "tools", "stream", "max_tokens"):
            self.assertEqual(result[key], data[key])

    def test_other_models_and_string_systems_are_untouched(self):
        for data in (
            {"model": "other-provider", "system": [{"type": "text", "text": "Keep blocks"}]},
            {"model": "subscription-coder", "system": "Keep string"},
            {"model": "subscription-coder", "messages": []},
        ):
            self.assertIs(normalize_system_blocks(data, {"subscription-coder"}), data)

    def test_unknown_content_fails_instead_of_losing_instructions(self):
        for block in (
            {"type": "image", "source": "not-supported"},
            {"type": "text", "text": "Keep me", "unknown_instruction": "Must not disappear"},
            {"type": "text", "text": None},
            "unexpected-block",
        ):
            with self.assertRaises(ValueError):
                normalize_system_blocks(
                    {"model": "subscription-coder", "system": [block]}, {"subscription-coder"}
                )


if __name__ == "__main__":
    unittest.main()

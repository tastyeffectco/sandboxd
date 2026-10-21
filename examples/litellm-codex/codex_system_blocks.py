"""Optional LiteLLM proxy callback; not loaded by sandboxd itself."""

import os

from litellm.integrations.custom_logger import CustomLogger
from system_blocks import normalize_system_blocks


class CodexSystemBlocks(CustomLogger):
    def __init__(self):
        super().__init__()
        self.aliases = frozenset(
            alias.strip()
            for alias in os.environ.get("SANDBOXD_CODEX_LITELLM_MODELS", "").split(",")
            if alias.strip()
        )
        if not self.aliases:
            raise ValueError(
                "Set SANDBOXD_CODEX_LITELLM_MODELS to the gateway aliases "
                "backed by your Codex account"
            )

    async def async_pre_call_hook(self, user_api_key_dict, cache, data, call_type):
        return normalize_system_blocks(data, self.aliases)


proxy_handler_instance = CodexSystemBlocks()

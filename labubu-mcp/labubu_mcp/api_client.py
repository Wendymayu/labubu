"""HTTP client for the Labubu REST API."""
import httpx


class LabubuApiClient:
    """Async HTTP client wrapping the Labubu REST API (GET /api/v1/*)."""

    def __init__(self, base_url: str, transport=None):
        self.base_url = base_url.rstrip("/")
        self._transport = transport

    async def search_traces(self, **kwargs):
        """GET /api/v1/traces with query filters."""
        params = self._build_trace_params(kwargs)
        try:
            async with httpx.AsyncClient(transport=self._transport) as client:
                r = await client.get(
                    f"{self.base_url}/api/v1/traces",
                    params=params,
                    timeout=30.0,
                )
                r.raise_for_status()
                return r.json()
        except httpx.ConnectError:
            return {
                "error": (
                    f"Cannot connect to Labubu at {self.base_url}. "
                    "Is 'labubu serve' running?"
                )
            }
        except httpx.TimeoutException:
            return {
                "error": (
                    "Request timed out after 30s. "
                    "Try narrowing the time range or reducing the limit."
                )
            }

    async def get_trace_detail(self, trace_id: str):
        """GET /api/v1/traces/{trace_id}. Returns None if not found."""
        try:
            async with httpx.AsyncClient(transport=self._transport) as client:
                r = await client.get(
                    f"{self.base_url}/api/v1/traces/{trace_id}",
                    timeout=30.0,
                )
                if r.status_code == 404:
                    return None
                r.raise_for_status()
                return r.json()
        except httpx.ConnectError:
            return {
                "error": (
                    f"Cannot connect to Labubu at {self.base_url}. "
                    "Is 'labubu serve' running?"
                )
            }
        except httpx.TimeoutException:
            return {
                "error": (
                    "Request timed out after 30s. "
                    "Try narrowing the time range or reducing the limit."
                )
            }

    async def search_logs(self, **kwargs):
        """GET /api/v1/logs with query filters."""
        params = {}
        for key in ("trace_id", "severity", "event_name", "query", "start_time", "end_time"):
            if key in kwargs and kwargs[key] is not None:
                params[key] = kwargs[key]
        params["limit"] = min(kwargs.get("limit", 20), 50)
        params["offset"] = kwargs.get("offset", 0)
        try:
            async with httpx.AsyncClient(transport=self._transport) as client:
                r = await client.get(
                    f"{self.base_url}/api/v1/logs",
                    params=params,
                    timeout=30.0,
                )
                r.raise_for_status()
                return r.json()
        except httpx.ConnectError:
            return {
                "error": (
                    f"Cannot connect to Labubu at {self.base_url}. "
                    "Is 'labubu serve' running?"
                )
            }
        except httpx.TimeoutException:
            return {
                "error": (
                    "Request timed out after 30s. "
                    "Try narrowing the time range or reducing the limit."
                )
            }

    async def query_metrics(self, query: str, time: str = None):
        """GET /api/v1/query?query=...&time=..."""
        params = {"query": query}
        if time:
            params["time"] = time
        try:
            async with httpx.AsyncClient(transport=self._transport) as client:
                r = await client.get(
                    f"{self.base_url}/api/v1/query",
                    params=params,
                    timeout=30.0,
                )
                r.raise_for_status()
                return r.json()
        except httpx.ConnectError:
            return {
                "error": (
                    f"Cannot connect to Labubu at {self.base_url}. "
                    "Is 'labubu serve' running?"
                )
            }
        except httpx.TimeoutException:
            return {
                "error": (
                    "Request timed out after 30s. "
                    "Try narrowing the time range or reducing the limit."
                )
            }

    async def list_services(self):
        """GET /api/v1/services."""
        try:
            async with httpx.AsyncClient(transport=self._transport) as client:
                r = await client.get(
                    f"{self.base_url}/api/v1/services",
                    timeout=30.0,
                )
                r.raise_for_status()
                return r.json()
        except httpx.ConnectError:
            return {
                "error": (
                    f"Cannot connect to Labubu at {self.base_url}. "
                    "Is 'labubu serve' running?"
                )
            }
        except httpx.TimeoutException:
            return {
                "error": (
                    "Request timed out after 30s. "
                    "Try narrowing the time range or reducing the limit."
                )
            }

    @staticmethod
    def _build_trace_params(kwargs):
        """Map tool arguments to Labubu query string parameters."""
        params = {}
        mapping = {
            "status": "status",
            "service": "service",
            "query": "q",
            "start_time": "start",
            "end_time": "end",
            "min_duration_ms": "min_duration",
            "max_duration_ms": "max_duration",
        }
        for tool_key, api_key in mapping.items():
            if tool_key in kwargs and kwargs[tool_key] is not None:
                params[api_key] = kwargs[tool_key]
        params["page_size"] = min(kwargs.get("limit", 20), 50)
        page = kwargs.get("offset", 0) // params["page_size"] + 1 if kwargs.get("offset") else 1
        params["page"] = page
        return params

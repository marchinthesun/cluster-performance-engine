# nexusflow_sdk (Python)

Usage:

```python
from nexusflow_sdk import discover

t = discover()  # sysfs on Linux
print(t.dumps_indent())

# Same JSON shape as Go CLI:
# t = discover(prefer_cli=True)
```

Environment variable **`NEXUSFLOW_BIN`** points at the `nexusflow` binary for CLI-backed discovery.

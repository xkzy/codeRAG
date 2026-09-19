# Debugger adapter contract

Debugger integrations never execute debugger commands through MCP. A local, explicitly configured adapter may submit observations using `record_observation`, with the observed function/instruction as `subject_id`, source (`gdb`, `lldb`, or adapter name), timestamp, method, and confidence. This preserves runtime evidence without granting an agent shell or debugger control.

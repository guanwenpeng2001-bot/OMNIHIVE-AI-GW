package cursor

import "fmt"

// Cursor agent 2026.09.26-dd393fe exec_pb.js/mcp_exec_pb.js: these are
// metadata operations, distinct from an MCP tool execution on field 11.
const (
	execMCPState    = 36
	execMCPPrecheck = 42
)

func replyMCPMetadata(payload []byte, tools []ToolDefinition) (message, bool, error) {
	outer, err := fields(payload)
	if err != nil {
		return nil, false, err
	}
	exec := findField(outer, 2)
	if exec.number == 0 {
		return nil, false, nil
	}
	if exec.wire != wireBytes {
		return nil, true, fmt.Errorf("cursor: malformed exec")
	}
	values, err := fields(exec.data)
	if err != nil {
		return nil, true, err
	}
	operation := findField(values, execMCPState)
	if operation.number == 0 {
		operation = findField(values, execMCPPrecheck)
	}
	if operation.number == 0 {
		return nil, false, nil
	}
	id, execID := findField(values, 1), findField(values, 15)
	// Metadata identity uses proto3 scalars: omitted id/exec_id mean 0/"".
	if (id.number != 0 && (id.wire != wireVarint || id.integer > 0xffffffff)) || (execID.number != 0 && execID.wire != wireBytes) || operation.wire != wireBytes {
		return nil, true, errMalformedMCPMetadataIdentity
	}
	args, err := fields(operation.data)
	if err != nil {
		return nil, true, err
	}
	var result message
	if operation.number == execMCPState {
		servers := make(map[string]bool)
		for _, arg := range args {
			if arg.number == 1 {
				if arg.wire != wireBytes {
					return nil, true, errMalformedMCPServerFilter
				}
				servers[string(arg.data)] = true
			}
		}
		// kick_only needs no action: supplied definitions are already available.
		result.bytes(1, encodeMCPState(tools, servers))
	} else {
		provider, name := findField(args, 1), findField(args, 2)
		if (provider.number != 0 && provider.wire != wireBytes) || (name.number != 0 && name.wire != wireBytes) {
			return nil, true, errMalformedMCPPrecheck
		}
		for _, tool := range tools {
			if tool.Server == string(provider.data) && tool.Name == string(name.data) {
				result.number(1, 1)
				break
			}
		}
	}
	var reply, client message
	if id.integer != 0 {
		reply.number(1, id.integer)
	}
	if len(execID.data) != 0 {
		reply.bytes(15, execID.data)
	}
	reply.bytes(operation.number, result)
	client.bytes(2, reply)
	return client, true, nil
}

func encodeMCPState(tools []ToolDefinition, filter map[string]bool) message {
	servers := make(map[string]message)
	var order []string
	for _, tool := range tools {
		if len(filter) > 0 && !filter[tool.Server] {
			continue
		}
		server, exists := servers[tool.Server]
		if !exists {
			order = append(order, tool.Server)
			server.text(1, tool.Server)
			server.text(2, tool.Server)
		}
		server.bytes(5, encodeToolDefinition(tool))
		servers[tool.Server] = server
	}
	var success message
	for _, name := range order {
		success.bytes(1, servers[name])
	}
	return success
}

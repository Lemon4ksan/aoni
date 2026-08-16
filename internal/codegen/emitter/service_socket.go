// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package emitter

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/lemon4ksan/aoni/internal/codegen/ir"
)

func emitEventMethod(
	buf *bytes.Buffer,
	tracker *ImportTracker,
	_ *ir.RootIR,
	_ *ir.ServiceIR,
	clientStructName string,
	m *ir.MethodIR,
) {
	paramsStr := formatMethodParams(m.Params)
	returnsStr := formatMethodReturns(m.Return)

	fmt.Fprintf(buf, "func (c *%s) %s(%s) %s {\n", clientStructName, m.Name, paramsStr, returnsStr)

	handlerParamName := "handler"
	handlerElemType := ""

	for _, p := range m.Params {
		if strings.HasPrefix(p.GoType.Name, "func(") {
			handlerParamName = p.GoName
			handlerElemType = p.GoType.ElemType
			break
		}
	}

	opIDExpr := formatOpID(m.OpID, m.OpIDIsQuoted)
	fmt.Fprintf(buf, "\tunsub := c.transport.Subscribe(%s, func(raw []byte) {\n", opIDExpr)

	switch {
	case m.ReturnPipeline != nil && len(m.ReturnPipeline.Stages) > 0:
		buf.WriteString("\t\tstageIn := raw\n")

		for i, stage := range m.ReturnPipeline.Stages {
			switch stage.Type {
			case ir.StageProto:
				tracker.Add("google.golang.org/protobuf/proto")

				isPtr := strings.HasPrefix(handlerElemType, "*")
				baseType := strings.TrimPrefix(handlerElemType, "*")
				fmt.Fprintf(buf, "\t\tvar msg %s\n", baseType)
				buf.WriteString("\t\tif err := proto.Unmarshal(stageIn, &msg); err != nil {\n\t\t\treturn\n\t\t}\n")

				if isPtr {
					fmt.Fprintf(buf, "\t\t%s(&msg)\n", handlerParamName)
				} else {
					fmt.Fprintf(buf, "\t\t%s(msg)\n", handlerParamName)
				}

				buf.WriteString("\t})\n\n")
				buf.WriteString("\tc.mu.Lock()\n\tc.unregs = append(c.unregs, unsub)\n\tc.mu.Unlock()\n\n")
				buf.WriteString("\treturn unsub\n}\n\n")

				return

			case ir.StageJSON:
				tracker.Add("github.com/lemon4ksan/aoni/codec/decode")

				isPtr := strings.HasPrefix(handlerElemType, "*")
				baseType := strings.TrimPrefix(handlerElemType, "*")
				fmt.Fprintf(buf, "\t\tvar msg %s\n", baseType)
				buf.WriteString(
					"\t\tif err := decode.UnmarshalJSON(stageIn, &msg); err != nil {\n\t\t\treturn\n\t\t}\n",
				)

				if isPtr {
					fmt.Fprintf(buf, "\t\t%s(&msg)\n", handlerParamName)
				} else {
					fmt.Fprintf(buf, "\t\t%s(msg)\n", handlerParamName)
				}

				buf.WriteString("\t})\n\n")
				buf.WriteString("\tc.mu.Lock()\n\tc.unregs = append(c.unregs, unsub)\n\tc.mu.Unlock()\n\n")
				buf.WriteString("\treturn unsub\n}\n\n")

				return

			case ir.StageCustom:
				fmt.Fprintf(buf, "\t\tres, err := %s(stageIn)\n", stage.FuncExpr)
				buf.WriteString("\t\tif err != nil {\n\t\t\treturn\n\t\t}\n")
				fmt.Fprintf(buf, "\t\t%s(res)\n", handlerParamName)
				buf.WriteString("\t})\n\n")
				buf.WriteString("\tc.mu.Lock()\n\tc.unregs = append(c.unregs, unsub)\n\tc.mu.Unlock()\n\n")
				buf.WriteString("\treturn unsub\n}\n\n")

				return

			default:
				tracker.Add("github.com/lemon4ksan/aoni/codec/decode")
				fmt.Fprintf(
					buf,
					"\t\tstageOut%d, err := decode.ExtractAttr(stageIn, %q, %q)\n",
					i,
					stage.NamedArgs["css"],
					stage.NamedArgs["name"],
				)
				buf.WriteString("\t\tif err != nil {\n\t\t\treturn\n\t\t}\n")
				fmt.Fprintf(buf, "\t\tstageIn = stageOut%d\n", i)
			}
		}

	case isProtoType(handlerElemType):
		tracker.Add("google.golang.org/protobuf/proto")

		isPtr := strings.HasPrefix(handlerElemType, "*")
		baseType := strings.TrimPrefix(handlerElemType, "*")
		fmt.Fprintf(buf, "\t\tvar msg %s\n", baseType)
		buf.WriteString("\t\tif err := proto.Unmarshal(raw, &msg); err != nil {\n\t\t\treturn\n\t\t}\n")

		if isPtr {
			fmt.Fprintf(buf, "\t\t%s(&msg)\n", handlerParamName)
		} else {
			fmt.Fprintf(buf, "\t\t%s(msg)\n", handlerParamName)
		}

	default:
		fmt.Fprintf(buf, "\t\t%s(raw)\n", handlerParamName)
	}

	buf.WriteString("\t})\n\n")
	buf.WriteString("\tc.mu.Lock()\n\tc.unregs = append(c.unregs, unsub)\n\tc.mu.Unlock()\n\n")
	buf.WriteString("\treturn unsub\n}\n\n")
}

func emitRPCMethod(
	buf *bytes.Buffer,
	tracker *ImportTracker,
	_ *ir.RootIR,
	_ *ir.ServiceIR,
	clientStructName string,
	m *ir.MethodIR,
) {
	tracker.Add("context")

	paramsStr := formatMethodParams(m.Params)
	returnsStr := formatMethodReturns(m.Return)

	fmt.Fprintf(buf, "func (c *%s) %s(%s) %s {\n", clientStructName, m.Name, paramsStr, returnsStr)

	reqParamName := ""
	reqParamType := ""

	for _, p := range m.Params {
		if p.GoType.Name != "context.Context" && !p.GoType.IsVariadic {
			reqParamName = p.GoName
			reqParamType = p.GoType.Name
			break
		}
	}

	opIDExpr := formatOpID(m.OpID, m.OpIDIsQuoted)

	if reqParamName != "" {
		switch {
		case m.BodyPipeline != nil && len(m.BodyPipeline.Stages) > 0:
			switch m.BodyPipeline.Stages[0].Type {
			case ir.StageProto:
				fmt.Fprintf(buf, "\tpayloadBytes, err := proto.Marshal(%s)\n", reqParamName)
			case ir.StageJSON:
				fmt.Fprintf(buf, "\tpayloadBytes, err := json.Marshal(%s)\n", reqParamName)
			case ir.StageCustom:
				fmt.Fprintf(buf, "\tpayloadBytes, err := %s(%s)\n", m.BodyPipeline.Stages[0].FuncExpr, reqParamName)
			default:
				fmt.Fprintf(buf, "\tpayloadBytes, err := json.Marshal(%s)\n", reqParamName)
			}

		case isProtoType(reqParamType):
			fmt.Fprintf(buf, "\tpayloadBytes, err := proto.Marshal(%s)\n", reqParamName)
		case reqParamType == "[]byte":
			fmt.Fprintf(buf, "\tpayloadBytes := %s\n", reqParamName)
		default:
			fmt.Fprintf(buf, "\tpayloadBytes, err := json.Marshal(%s)\n", reqParamName)
		}

		if reqParamType != "[]byte" {
			if m.IsNotify || m.Operation == ir.OpNotify {
				buf.WriteString("\tif err != nil {\n\t\treturn err\n\t}\n\n")
			} else {
				buf.WriteString("\tif err != nil {\n\t\treturn nil, err\n\t}\n\n")
			}
		}
	} else {
		buf.WriteString("\tvar payloadBytes []byte\n\n")
	}

	if m.IsNotify || m.Operation == ir.OpNotify {
		fmt.Fprintf(buf, "\treturn c.transport.Notify(ctx, %s, payloadBytes)\n}\n\n", opIDExpr)
		return
	}

	fmt.Fprintf(buf, "\trawResp, err := c.transport.Invoke(ctx, %s, payloadBytes)\n", opIDExpr)
	buf.WriteString("\tif err != nil {\n\t\treturn nil, err\n\t}\n\n")

	// Decode response
	resultType := ""
	if m.Return != nil && m.Return.SuccessType.Name != "" {
		resultType = m.Return.SuccessType.Name
	}

	if resultType == "" || resultType == "error" || m.Return.IsVoid {
		buf.WriteString("\treturn nil\n}\n\n")
		return
	}

	switch {
	case m.ReturnPipeline != nil && len(m.ReturnPipeline.Stages) > 0:
		stage := m.ReturnPipeline.Stages[len(m.ReturnPipeline.Stages)-1]
		switch stage.Type {
		case ir.StageProto:
			isPtr := strings.HasPrefix(resultType, "*")
			baseType := strings.TrimPrefix(resultType, "*")
			fmt.Fprintf(buf, "\tvar result %s\n", baseType)
			buf.WriteString("\tif err := proto.Unmarshal(rawResp, &result); err != nil {\n\t\treturn nil, err\n\t}\n")

			if isPtr {
				buf.WriteString("\treturn &result, nil\n")
			} else {
				buf.WriteString("\treturn result, nil\n")
			}

		case ir.StageJSON:
			isPtr := strings.HasPrefix(resultType, "*")
			baseType := strings.TrimPrefix(resultType, "*")
			fmt.Fprintf(buf, "\tvar result %s\n", baseType)
			buf.WriteString(
				"\tif err := decode.UnmarshalJSON(rawResp, &result); err != nil {\n\t\treturn nil, err\n\t}\n",
			)

			if isPtr {
				buf.WriteString("\treturn &result, nil\n")
			} else {
				buf.WriteString("\treturn result, nil\n")
			}

		case ir.StageCustom:
			fmt.Fprintf(buf, "\treturn %s(rawResp)\n", stage.FuncExpr)
		default:
			buf.WriteString("\treturn rawResp, nil\n")
		}

	case isProtoType(resultType):
		isPtr := strings.HasPrefix(resultType, "*")
		baseType := strings.TrimPrefix(resultType, "*")
		fmt.Fprintf(buf, "\tvar result %s\n", baseType)
		buf.WriteString("\tif err := proto.Unmarshal(rawResp, &result); err != nil {\n\t\treturn nil, err\n\t}\n")

		if isPtr {
			buf.WriteString("\treturn &result, nil\n")
		} else {
			buf.WriteString("\treturn result, nil\n")
		}

	case resultType == "[]byte":
		buf.WriteString("\treturn rawResp, nil\n")
	default:
		isPtr := strings.HasPrefix(resultType, "*")
		baseType := strings.TrimPrefix(resultType, "*")
		fmt.Fprintf(buf, "\tvar result %s\n", baseType)
		buf.WriteString("\tif err := decode.UnmarshalJSON(rawResp, &result); err != nil {\n\t\treturn nil, err\n\t}\n")

		if isPtr {
			buf.WriteString("\treturn &result, nil\n")
		} else {
			buf.WriteString("\treturn result, nil\n")
		}
	}

	buf.WriteString("}\n\n")
}

func isNumeric(s string) bool {
	if s == "" {
		return false
	}

	for _, r := range s {
		if !unicode.IsDigit(r) && r != '-' && r != '+' {
			return false
		}
	}

	return true
}

func formatOpID(opID string, isQuoted bool) string {
	if isQuoted {
		return strconv.Quote(opID)
	}

	if isNumeric(opID) {
		return opID
	}

	return opID
}

func emitSocketService(buf *bytes.Buffer, tracker *ImportTracker, _ *ir.RootIR, svc *ir.ServiceIR) {
	tracker.Add("context")
	tracker.Add("errors")
	tracker.Add("sync")
	tracker.Add("sync/atomic")
	tracker.Add("github.com/lemon4ksan/aoni/realtime/socket")
	tracker.Add("github.com/lemon4ksan/aoni/realtime/socket/connector")
	tracker.Add("github.com/lemon4ksan/aoni/realtime/socket/dispatcher")
	tracker.Add("github.com/lemon4ksan/aoni/realtime/socket/processor")

	clientStructName := lowerFirst(svc.Name) + "Impl"

	endpointType := "string"
	packetType := "any"
	opCodeType := "int"
	jobIDType := "uint64"

	if svc.SocketConfig != nil {
		if svc.SocketConfig.EndpointType != "" {
			endpointType = svc.SocketConfig.EndpointType
		}

		if svc.SocketConfig.PacketType != "" {
			packetType = svc.SocketConfig.PacketType
		}

		if svc.SocketConfig.OpCodeType != "" {
			opCodeType = svc.SocketConfig.OpCodeType
		}

		if svc.SocketConfig.JobIDType != "" {
			jobIDType = svc.SocketConfig.JobIDType
		}
	}

	configTypeName := svc.Name + "Config"
	if svc.Name == "Socket" || svc.Name == "Client" {
		configTypeName = "Config"
	}

	fmt.Fprintf(buf, "// %s configures the %s socket subsystem.\n", configTypeName, svc.Name)
	fmt.Fprintf(buf, "type %s struct {\n", configTypeName)
	fmt.Fprintf(buf, "\tConnector connector.Config[%s]\n", endpointType)
	fmt.Fprintf(buf, "\tProcessor processor.Config\n")
	fmt.Fprintf(buf, "\tDispatcher dispatcher.Config\n")
	fmt.Fprintf(buf, "\tFramer socket.Framer\n")
	fmt.Fprintf(buf, "\tCipher socket.Cipher\n")
	fmt.Fprintf(buf, "\tDecode processor.DecodeFunc[%s]\n", packetType)
	fmt.Fprintf(buf, "\tExtractor dispatcher.Extractor[%s, %s, %s]\n", opCodeType, jobIDType, packetType)
	fmt.Fprintf(buf, "}\n\n")

	fmt.Fprintf(buf, "type %s struct {\n", clientStructName)
	fmt.Fprintf(buf, "\tcfg %s\n", configTypeName)
	fmt.Fprintf(buf, "\tconn *connector.Connector[%s]\n", endpointType)
	fmt.Fprintf(buf, "\tproc *processor.Processor[%s]\n", packetType)
	fmt.Fprintf(buf, "\tdispatch *dispatcher.Dispatcher[%s, %s, %s]\n", opCodeType, jobIDType, packetType)
	fmt.Fprintf(buf, "\tclosed atomic.Bool\n")
	fmt.Fprintf(buf, "\tmu sync.RWMutex\n")
	fmt.Fprintf(buf, "\theartbeatCancel context.CancelFunc\n")
	fmt.Fprintf(buf, "}\n\n")

	constructorName := "New" + svc.Name
	if svc.Name == "Socket" || svc.Name == "Client" {
		constructorName = "New"
	}

	fmt.Fprintf(buf, "// %s initializes a new %s socket instance.\n", constructorName, svc.Name)
	fmt.Fprintf(buf, "func %s(cfg %s) %s {\n", constructorName, configTypeName, svc.Name)
	fmt.Fprintf(buf, "\ts := &%s{cfg: cfg}\n", clientStructName)
	fmt.Fprintf(buf, "\tif cfg.Framer != nil {\n\t\ts.cfg.Connector.Framer = cfg.Framer\n\t}\n")
	fmt.Fprintf(buf, "\tif cfg.Cipher != nil {\n\t\ts.cfg.Connector.Cipher = cfg.Cipher\n\t}\n")
	fmt.Fprintf(buf, "\ts.conn = connector.New[%s](s.cfg.Connector)\n", endpointType)
	fmt.Fprintf(
		buf,
		"\ts.dispatch = dispatcher.New[%s, %s, %s](s.cfg.Dispatcher, s.conn, s.cfg.Extractor)\n",
		opCodeType,
		jobIDType,
		packetType,
	)
	fmt.Fprintf(
		buf,
		"\ts.proc = processor.New[%s](s.cfg.Processor, s.conn.C(), s.dispatch, s.cfg.Decode)\n",
		packetType,
	)
	fmt.Fprintf(buf, "\treturn s\n}\n\n")

	for _, m := range svc.Methods {
		switch m.Name {
		case "Connect":
			fmt.Fprintf(
				buf,
				"func (s *%s) Connect(ctx context.Context, endpoint %s) error {\n",
				clientStructName,
				endpointType,
			)
			buf.WriteString("\tif s.closed.Load() {\n\t\treturn errors.New(\"socket: closed\")\n\t}\n")
			buf.WriteString("\ts.proc.Start()\n")
			buf.WriteString("\treturn s.conn.Connect(ctx, endpoint)\n}\n\n")

		case "Disconnect":
			fmt.Fprintf(buf, "func (s *%s) Disconnect() error {\n", clientStructName)
			buf.WriteString("\treturn s.conn.Disconnect()\n}\n\n")

		case "Close":
			fmt.Fprintf(buf, "func (s *%s) Close() error {\n", clientStructName)
			buf.WriteString("\tif s.closed.Swap(true) {\n\t\treturn nil\n\t}\n")
			buf.WriteString("\ts.mu.Lock()\n")
			buf.WriteString(
				"\tif s.heartbeatCancel != nil {\n\t\ts.heartbeatCancel()\n\t\ts.heartbeatCancel = nil\n\t}\n",
			)
			buf.WriteString("\ts.mu.Unlock()\n")
			buf.WriteString("\tvar errs []error\n")
			buf.WriteString("\terrs = append(errs, s.conn.Close())\n")
			buf.WriteString("\ts.proc.Stop()\n")
			buf.WriteString("\terrs = append(errs, s.dispatch.Close())\n")
			buf.WriteString("\treturn errors.Join(errs...)\n}\n\n")

		case "IsConnected":
			fmt.Fprintf(buf, "func (s *%s) IsConnected() bool {\n", clientStructName)
			buf.WriteString("\treturn s.conn.IsConnected() && !s.closed.Load()\n}\n\n")

		case "Connector":
			fmt.Fprintf(
				buf,
				"func (s *%s) Connector() *connector.Connector[%s] {\n\treturn s.conn\n}\n\n",
				clientStructName,
				endpointType,
			)

		case "Dispatcher":
			fmt.Fprintf(
				buf,
				"func (s *%s) Dispatcher() *dispatcher.Dispatcher[%s, %s, %s] {\n\treturn s.dispatch\n}\n\n",
				clientStructName,
				opCodeType,
				jobIDType,
				packetType,
			)

		default:
			switch {
			case m.IsEvent || m.Operation == ir.OpEvent || strings.HasPrefix(m.Name, "Register"):
				if len(m.Params) >= 2 {
					p0Type := formatType(m.Params[0].GoType)
					p1Type := formatType(m.Params[1].GoType)

					if strings.Contains(strings.ToLower(m.Name), "service") || p0Type == "string" {
						fmt.Fprintf(
							buf,
							"func (s *%s) %s(%s string, %s %s) {\n",
							clientStructName,
							m.Name,
							m.Params[0].GoName,
							m.Params[1].GoName,
							p1Type,
						)
						fmt.Fprintf(
							buf,
							"\ts.dispatch.RegisterMethodHandler(%s, %s)\n}\n\n",
							m.Params[0].GoName,
							m.Params[1].GoName,
						)
					} else {
						fmt.Fprintf(
							buf,
							"func (s *%s) %s(%s %s, %s %s) {\n",
							clientStructName,
							m.Name,
							m.Params[0].GoName,
							p0Type,
							m.Params[1].GoName,
							p1Type,
						)
						fmt.Fprintf(
							buf,
							"\ts.dispatch.RegisterHandler(%s, %s)\n}\n\n",
							m.Params[0].GoName,
							m.Params[1].GoName,
						)
					}
				}

			case m.Operation == ir.OpRPC || strings.HasSuffix(m.Name, "Sync"):
				retType := formatMethodReturns(m.Return)
				paramsStr := formatMethodParams(m.Params)
				fmt.Fprintf(buf, "func (s *%s) %s(%s) %s {\n", clientStructName, m.Name, paramsStr, retType)
				buf.WriteString("\tif s.closed.Load() {\n")

				if m.Return != nil && !m.Return.IsVoid {
					tName := formatType(m.Return.SuccessType)
					fmt.Fprintf(buf, "\t\tvar zero %s\n\t\treturn zero, errors.New(\"socket: closed\")\n", tName)
				} else {
					buf.WriteString("\t\treturn errors.New(\"socket: closed\")\n")
				}

				buf.WriteString("\t}\n")
				buf.WriteString("\tjobID := s.dispatch.NextJobID()\n")
				buf.WriteString("\treturn s.dispatch.SendSync(ctx, jobID, req)\n}\n\n")

			default:
				paramsStr := formatMethodParams(m.Params)
				retType := formatMethodReturns(m.Return)
				fmt.Fprintf(buf, "func (s *%s) %s(%s) %s {\n", clientStructName, m.Name, paramsStr, retType)
				buf.WriteString("\tif s.closed.Load() {\n\t\treturn errors.New(\"socket: closed\")\n\t}\n")
				buf.WriteString("\treturn s.conn.Send(ctx, req)\n}\n\n")
			}
		}
	}

	if svc.SocketConfig != nil && svc.SocketConfig.Heartbeat != nil {
		tracker.Add("time")
		fmt.Fprintf(buf, "// StartHeartbeat initiates a background heartbeat loop.\n")
		fmt.Fprintf(buf, "func (s *%s) StartHeartbeat(interval time.Duration) error {\n", clientStructName)
		buf.WriteString("\tif s.closed.Load() {\n\t\treturn errors.New(\"socket: closed\")\n\t}\n")
		buf.WriteString("\ts.mu.Lock()\n")
		buf.WriteString("\tif s.heartbeatCancel != nil {\n\t\ts.heartbeatCancel()\n\t}\n")
		buf.WriteString("\tctx, cancel := context.WithCancel(context.Background())\n")
		buf.WriteString("\ts.heartbeatCancel = cancel\n")
		buf.WriteString("\ts.mu.Unlock()\n\n")
		buf.WriteString("\tgo func() {\n")
		buf.WriteString("\t\tticker := time.NewTicker(interval)\n")
		buf.WriteString("\t\tdefer ticker.Stop()\n")
		buf.WriteString("\t\tfor {\n")
		buf.WriteString("\t\t\tselect {\n")
		buf.WriteString("\t\t\tcase <-ticker.C:\n")
		buf.WriteString("\t\t\t\tif !s.IsConnected() {\n\t\t\t\t\tcontinue\n\t\t\t\t}\n")
		buf.WriteString("\t\t\tcase <-ctx.Done():\n\t\t\t\treturn\n")
		buf.WriteString("\t\t\t}\n\t\t}\n\t}()\n")
		buf.WriteString("\treturn nil\n}\n\n")
	}
}

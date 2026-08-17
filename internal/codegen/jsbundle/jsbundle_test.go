// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package jsbundle_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/internal/codegen/jsbundle"
)

func TestScanBytes_RPCEndpoints(t *testing.T) {
	jsCode := []byte(`
		const rpc1 = "/$rpc/google.internal.alkali.applications.makersuite.v1.MakerSuiteService/ListModels";
		const rpc2 = "/$rpc/google.internal.alkali.applications.makersuite.v1.MakerSuiteService/CountTokens";
		const twirp = "/twirp/twitch.example.Haberdasher/MakeHat";
		const rest = "/api/v1/users/profiles";
		const trpc = "/trpc/user.getById";
		const gql = "/graphql";
	`)

	res := jsbundle.ScanBytes(jsCode, "test.js")
	require.NotNil(t, res)
	require.Len(t, res.Endpoints, 6)

	require.Equal(t, "/$rpc/google.internal.alkali.applications.makersuite.v1.MakerSuiteService/ListModels", res.Endpoints[0].Path)
	require.Equal(t, "grpc-web", res.Endpoints[0].Protocol)
	require.Equal(t, "POST", res.Endpoints[0].HTTPMethod)

	require.Equal(t, "/twirp/twitch.example.Haberdasher/MakeHat", res.Endpoints[2].Path)
	require.Equal(t, "twirp", res.Endpoints[2].Protocol)

	require.Equal(t, "/api/v1/users/profiles", res.Endpoints[3].Path)
	require.Equal(t, "rest", res.Endpoints[3].Protocol)

	require.Equal(t, "/trpc/user.getById", res.Endpoints[4].Path)
	require.Equal(t, "trpc", res.Endpoints[4].Protocol)

	require.Equal(t, "/graphql", res.Endpoints[5].Path)
	require.Equal(t, "graphql", res.Endpoints[5].Protocol)
}

func TestScanBytes_StandardJSPBGettersAndSetters(t *testing.T) {
	jsCode := []byte(`
		proto.my.package.User.prototype.getId = function() {
			return jspb.Message.getField(this, 1);
		};
		proto.my.package.User.prototype.setId = function(a) {
			return jspb.Message.setField(this, 1, a);
		};
		proto.my.package.User.prototype.getDisplayName = function() {
			return jspb.Message.getField(this, 2);
		};
	`)

	res := jsbundle.ScanBytes(jsCode, "user.js")
	require.NotNil(t, res)
	require.NotEmpty(t, res.Messages)

	var userMsg *jsbundle.MessageDescriptor
	for _, m := range res.Messages {
		if len(m.Fields) > 0 {
			userMsg = m
			break
		}
	}

	require.NotNil(t, userMsg)
	require.Equal(t, "Id", userMsg.Fields[1].Name)
	require.Equal(t, "DisplayName", userMsg.Fields[2].Name)
}

func TestScanBytes_MinifiedClosureAccessors(t *testing.T) {
	jsCode := []byte(`
		_.Kw.prototype.yj=_.da(54,function(){return _.Il(this,_.aq,3)});
		_.$w.prototype.yj=_.da(53,function(){return _.Il(this,_.aq,13)});
		_.ry.prototype.yj=_.da(52,function(){return _.Ql(this,7)});
		_.By.prototype.yj=_.da(51,function(){return _.Il(this,_.rw,1)});
	`)

	res := jsbundle.ScanBytes(jsCode, "m=AgQvWc.js")
	require.NotNil(t, res)
	require.NotEmpty(t, res.Messages)

	kwMsg, ok := res.Messages["Kw"]
	require.True(t, ok)
	require.Equal(t, 3, kwMsg.Fields[3].Index)
	require.True(t, kwMsg.Fields[3].IsNested)
	require.Equal(t, "aq", kwMsg.Fields[3].SubMsgType)

	dollarMsg, ok := res.Messages["$w"]
	require.True(t, ok)
	require.Equal(t, 13, dollarMsg.Fields[13].Index)
	require.True(t, dollarMsg.Fields[13].IsNested)
	require.Equal(t, "aq", dollarMsg.Fields[13].SubMsgType)

	ryMsg, ok := res.Messages["ry"]
	require.True(t, ok)
	require.Equal(t, 7, ryMsg.Fields[7].Index)
	require.False(t, ryMsg.Fields[7].IsNested)
}

func TestScanBytes_BinaryReaderSwitch(t *testing.T) {
	jsCode := []byte(`
		while (reader.nextField()) {
			if (reader.isEndGroup()) break;
			var field = reader.getFieldNumber();
			switch (field) {
				case 1:
					var value = reader.readString();
					msg.setName(value);
					break;
				case 2:
					var value = reader.readInt64();
					msg.setCount(value);
					break;
				case 3:
					var value = reader.readBool();
					msg.setEnabled(value);
					break;
			}
		}
	`)

	res := jsbundle.ScanBytes(jsCode, "binary.js")
	require.NotNil(t, res)
	require.NotEmpty(t, res.Messages)

	binMsg := res.Messages["BinaryReaderSchema"]
	require.NotNil(t, binMsg)
	require.Equal(t, "string", binMsg.Fields[1].GoType)
	require.Equal(t, "int64", binMsg.Fields[2].GoType)
	require.Equal(t, "bool", binMsg.Fields[3].GoType)
}

func TestScanBytes_Enums(t *testing.T) {
	jsCode := []byte(`
		var Status;
		(function (Status) {
			Status[Status["UNKNOWN"] = 0] = "UNKNOWN";
			Status[Status["ACTIVE"] = 1] = "ACTIVE";
			Status[Status["SUSPENDED"] = 2] = "SUSPENDED";
		})(Status || (Status = {}));
	`)

	res := jsbundle.ScanBytes(jsCode, "enums.js")
	require.NotNil(t, res)
	require.NotEmpty(t, res.Enums)

	enum := res.Enums["GlobalEnum"]
	require.NotNil(t, enum)
	require.Equal(t, "UNKNOWN", enum.Values[0])
	require.Equal(t, "ACTIVE", enum.Values[1])
	require.Equal(t, "SUSPENDED", enum.Values[2])
}

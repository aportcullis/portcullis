package connectapi

import (
	"errors"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// ExecutionJSONCodec rejects unknown JSON fields without echoing supplied values.
type ExecutionJSONCodec struct{}

// Name selects the Connect JSON codec.
func (ExecutionJSONCodec) Name() string { return "json" }

// Marshal encodes the protobuf response with the standard wire representation.
func (ExecutionJSONCodec) Marshal(value any) ([]byte, error) {
	message, ok := value.(proto.Message)
	if !ok {
		return nil, errors.New("invalid execution message")
	}
	return protojson.Marshal(message)
}

// Unmarshal refuses payload replacement and malformed values before execution.
func (ExecutionJSONCodec) Unmarshal(data []byte, value any) error {
	message, ok := value.(proto.Message)
	if !ok {
		return errors.New("invalid execution message")
	}
	if err := protojson.Unmarshal(data, message); err != nil {
		return errors.New("invalid execution message")
	}
	return nil
}

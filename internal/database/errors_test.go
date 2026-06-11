package database

import (
	"errors"
	"fmt"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestIsNotFoundError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"grpc NotFound", status.Error(codes.NotFound, "not found"), true},
		{"grpc FailedPrecondition", status.Error(codes.FailedPrecondition, ""), false},
		{"grpc Internal", status.Error(codes.Internal, ""), false},
		{"regular error", errors.New("not found"), false},
		{"nil", nil, false},
		{"wrapped grpc NotFound", fmt.Errorf("wrap: %w", status.Error(codes.NotFound, "")), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsNotFoundError(tt.err); got != tt.want {
				t.Errorf("IsNotFoundError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsPreconditionFailedError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"grpc FailedPrecondition", status.Error(codes.FailedPrecondition, "stale"), true},
		{"grpc NotFound", status.Error(codes.NotFound, ""), false},
		{"regular error", errors.New("precondition failed"), false},
		{"nil", nil, false},
		{"wrapped grpc FailedPrecondition", fmt.Errorf("wrap: %w", status.Error(codes.FailedPrecondition, "")), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsPreconditionFailedError(tt.err); got != tt.want {
				t.Errorf("IsPreconditionFailedError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewNotFoundError_IsClassified(t *testing.T) {
	err := NewNotFoundError()
	if !IsNotFoundError(err) {
		t.Error("NewNotFoundError() not classified as NotFound")
	}
	if IsPreconditionFailedError(err) {
		t.Error("NewNotFoundError() classified as PreconditionFailed")
	}
}

func TestNewPreconditionFailedError_IsClassified(t *testing.T) {
	err := NewPreconditionFailedError()
	if !IsPreconditionFailedError(err) {
		t.Error("NewPreconditionFailedError() not classified as PreconditionFailed")
	}
	if IsNotFoundError(err) {
		t.Error("NewPreconditionFailedError() classified as NotFound")
	}
}

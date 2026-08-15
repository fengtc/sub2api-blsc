package service

import "testing"

func TestProvideAccountTestServiceInjectsCopilotTokenProvider(t *testing.T) {
	provider := NewCopilotTokenProvider(nil)

	service := ProvideAccountTestService(
		nil,
		nil,
		nil,
		nil,
		nil,
		provider,
		nil,
		nil,
		nil,
		nil,
		nil,
	)

	if service.copilotTokenProvider != provider {
		t.Fatal("CopilotTokenProvider was not injected into AccountTestService")
	}
}

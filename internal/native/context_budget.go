package native

// GenerationContextTokens exposes verified pack metadata for bounded prompt
// planning without loading model weights or admitting a heavy worker.
func (s *Supervisor) GenerationContextTokens(packID string) (int, error) {
	p, _, release, err := s.manager.lease(packID)
	if err != nil {
		return 0, err
	}
	defer release()
	if p.ContextTokens > 32768 {
		return 32768, nil
	}
	return p.ContextTokens, nil
}

package verifier

import (
	"dhtbacked/auth"
	"dhtbacked/dht"
)

func (v *Verifier) preparedParent(tok *auth.Token, s *session, r dht.ParentRequest) ([]byte, error) {
	if b, ok := s.fetched[r.Key]; ok {
		return b, nil
	}
	return v.proof.GetParent(tok, r)
}
func (v *Verifier) preparedCert(tok *auth.Token, s *session, r dht.CertRequest) ([]byte, error) {
	if b, ok := s.fetched[r.Key]; ok {
		return b, nil
	}
	return v.proof.GetCert(tok, r)
}
func (v *Verifier) preparedRoot(tok *auth.Token, s *session, r dht.RootRequest) ([]byte, error) {
	if b, ok := s.fetched[r.Key]; ok {
		return b, nil
	}
	return v.proof.GetRoot(tok, r)
}

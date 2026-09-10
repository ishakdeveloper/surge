package fake

import "context"

// AccountSession returns a secret that no embedded component can use. The
// fake has no Stripe account behind it, and the web app does not render the
// components without a publishable key — which a fake-mode laptop has none of.
func (p *Processor) AccountSession(_ context.Context, accountID string) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.failing(); err != nil {
		return "", err
	}
	return "accs_fake_" + accountID + "_secret_fake", nil
}

// DashboardLink points nowhere a browser can go, for the same reason.
func (p *Processor) DashboardLink(_ context.Context, accountID string) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.failing(); err != nil {
		return "", err
	}
	return "fake://express/" + accountID, nil
}

package database

import "fmt"

// PoolLimits applies to one application replica, not the whole deployment.
type PoolLimits struct {
	MaxConns int32
	MinConns int32
}

func (p PoolLimits) Validate() error {
	if p.MaxConns < 1 {
		return fmt.Errorf("database maximum connections must be positive")
	}
	if p.MinConns < 0 || p.MinConns > p.MaxConns {
		return fmt.Errorf("database minimum connections must be between zero and the maximum")
	}
	return nil
}

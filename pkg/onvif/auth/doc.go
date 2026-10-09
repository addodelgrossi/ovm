// Package auth implements ONVIF UsernameToken generation, camera clock-offset
// sampling, HTTP Digest authentication, explicit Basic policy, and
// credential-free per-device strategy state.
//
// Credentials are supplied ephemerally by callers. This package does not
// persist them and does not depend on application, storage, CLI, or UI code.
package auth

package controller

import ctrl "sigs.k8s.io/controller-runtime"

func SetupReloaderController(mgr ctrl.Manager) error {
	if err := setupSecretController(mgr); err != nil {
		return err
	}

	if err := setupConfigMapController(mgr); err != nil {
		return err
	}

	return nil
}

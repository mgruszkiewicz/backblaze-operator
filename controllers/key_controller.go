/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"fmt"
	"os"
	"reflect"

	"github.com/go-logr/logr"
	b2v1alpha1 "github.com/ihyoudou/backblaze-operator/api/v1alpha1"
	"github.com/ihyoudou/go-backblaze"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const keyFinalizer = "key.b2.issei.space/finalizer"

// KeyReconciler reconciles a Key object
type KeyReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=b2.issei.space,resources=keys,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=b2.issei.space,resources=keys/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=b2.issei.space,resources=keys/finalizers,verbs=update
//+kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *KeyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("key", req.NamespacedName)
	key := &b2v1alpha1.Key{}

	if err := r.Get(ctx, types.NamespacedName{Name: req.Name, Namespace: req.Namespace}, key); err != nil {
		if errors.IsNotFound(err) {
			// object not found, could have been deleted after
			// reconcile request, hence don't requeue
			return ctrl.Result{}, nil
		}

		return ctrl.Result{}, err
	}

	if !controllerutil.ContainsFinalizer(key, keyFinalizer) {
		log.Info("Adding Finalizer")
		controllerutil.AddFinalizer(key, keyFinalizer)
		return ctrl.Result{}, r.Update(ctx, key)
	}

	if !key.DeletionTimestamp.IsZero() {
		log.Info("key is being deleted")
		return r.reconcileDelete(ctx, key)
	}
	r.reconcileCreate(ctx, key)

	return ctrl.Result{}, nil
}

func (r *KeyReconciler) reconcileCreate(ctx context.Context, key *b2v1alpha1.Key) (ctrl.Result, error) {
	// Create or update the key
	if err := r.createOrUpdateKey(ctx, key); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *KeyReconciler) createKeySecret(key *b2v1alpha1.Key, appkey *backblaze.ApplicationKeyResponse) *corev1.Secret {

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      key.Spec.WriteConnectionSecretToRef.Name,
			Namespace: key.Spec.WriteConnectionSecretToRef.Namespace,
		},
		Data: map[string][]byte{
			"bucketName": []byte(key.Spec.BucketName),
			"endpoint":   []byte(fmt.Sprintf("s3.%s.backblazeb2.com", string(os.Getenv("B2_REGION")))),
			"keyName":    []byte(appkey.KeyName),
			// AWS S3 compatibile variables
			"AWS_ACCESS_KEY_ID":     []byte(appkey.ApplicationKeyId),
			"AWS_SECRET_ACCESS_KEY": []byte(appkey.ApplicationKey),
		},
	}
}

func (r *KeyReconciler) createOrUpdateKey(ctx context.Context, key *b2v1alpha1.Key) error {
	log := r.Log.WithValues("key", key.Namespace)
	// Check if the key exists
	if err := r.Get(ctx, types.NamespacedName{Name: key.Name, Namespace: key.Namespace}, key); err != nil {
		if !errors.IsNotFound(err) {
			return fmt.Errorf("Unable to fetch Key: %v", err)
		}
	}

	// Checking if backblaze secrets are set
	if os.Getenv("B2_APPLICATION_ID") == "" || os.Getenv("B2_APPLICATION_KEY") == "" {
		log.Info("B2_APPLICATION_ID or B2_APPLICATION_KEY not set")
	}
	// Initializing backblaze client
	b2, _ := backblaze.NewB2(backblaze.Credentials{
		AccountID:      os.Getenv("B2_APPLICATION_ID"),
		ApplicationKey: os.Getenv("B2_APPLICATION_KEY"),
	})

	// If key was not already reconciled (most likely new CR)
	if !key.Status.Reconciled {
		var bucketId string

		// Checking which setting was set, BucketId vs BucketName
		// If BucketName is set, we need to get BucketId from backblaze
		// TODO: check if bucket already exist
		if key.Spec.BucketId == "" {
			bucket_b2, err := b2.Bucket(key.Spec.BucketName)
			if err != nil {
				log.Error(err, "Failed to get Bucket ", key.Spec.BucketName)
			}
			bucketId = bucket_b2.ID
		} else {
			bucketId = key.Spec.BucketId
		}

		// Creating application key
		applicationKeyCreate, err := b2.CreateApplicationKey(&backblaze.CreateKeyRequest{
			KeyName:      key.Name,
			Capabilities: key.Spec.Capabilities,
			BucketId:     bucketId,
		})
		if err != nil {
			log.Error(err, "Failed to create application key")
		}

		// Updating CRD status
		key.Status.Reconciled = true
		key.Status.BucketId = bucketId
		key.Status.KeyId = applicationKeyCreate.ApplicationKeyId
		key.Status.AtProvider = key.Spec
		r.Status().Update(ctx, key)

		// Creating secret with credentials
		secret := r.createKeySecret(key, applicationKeyCreate)

		if err := r.Create(ctx, secret); err != nil {
			log.Error(err, "Failed to create new Secret", "Secret.Namespace", secret.Namespace, "Deployment.Name", secret.Name)
		}

		return nil
	} else {
		// Backblaze doesn't support updating keys, so we need to recreate it
		if !reflect.DeepEqual(key.Spec, key.Status.AtProvider) {
			log.Info("Key resource exist on cluster, updating state")
			r.reconcileDelete(ctx, key)
			key.Status.Reconciled = false
			r.Status().Update(ctx, key)
			r.createOrUpdateKey(ctx, key)
		}

	}

	return nil
}

func (r *KeyReconciler) reconcileDelete(ctx context.Context, key *b2v1alpha1.Key) (ctrl.Result, error) {

	log := r.Log.WithValues("key", key.Namespace)
	log.Info("Removing Key")
	// Checking if backblaze secrets are set
	if os.Getenv("B2_APPLICATION_ID") == "" || os.Getenv("B2_APPLICATION_KEY") == "" {
		log.Info("B2_APPLICATION_ID or B2_APPLICATION_KEY not set")
	}
	// Initializing backblaze client
	b2, _ := backblaze.NewB2(backblaze.Credentials{
		AccountID:      os.Getenv("B2_APPLICATION_ID"),
		ApplicationKey: os.Getenv("B2_APPLICATION_KEY"),
	})

	// Retriving existing secret
	existing_secret := &corev1.Secret{}
	if err := r.Client.Get(ctx, types.NamespacedName{
		Name:      key.Spec.WriteConnectionSecretToRef.Name,
		Namespace: key.Spec.WriteConnectionSecretToRef.Namespace},
		existing_secret,
	); err != nil {
		log.Error(err, "Failed to get existing secret at cluster")
	} else {
		// Remove secret
		if err := r.Delete(ctx, existing_secret); err != nil {
			log.Error(err, "Failed to remove secret")
		} else {
			log.Info("Deleted secret at cluster")
		}
	}

	// Deleting application key on b2
	_, err := b2.DeleteApplicationKey(key.Status.KeyId)
	if err != nil {
		log.Error(err, "Failed to delete application key")
	} else {
		log.Info("Deleted Application Key at provider")
	}

	// Remove the finalizer and update the object
	controllerutil.RemoveFinalizer(key, keyFinalizer)
	if err := r.Update(ctx, key); err != nil {
		return ctrl.Result{}, fmt.Errorf("Error removing finalizer: %v", err)
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *KeyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&b2v1alpha1.Key{}).
		Complete(r)
}

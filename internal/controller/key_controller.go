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

package controller

import (
	"context"
	"fmt"
	"os"
	"reflect"

	"github.com/go-logr/logr"
	b2v1alpha2 "github.com/ihyoudou/backblaze-operator/api/v1alpha2"
	"github.com/ihyoudou/go-backblaze"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
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
	l := log.FromContext(ctx)
	key := &b2v1alpha2.Key{}

	if err := r.Get(ctx, types.NamespacedName{Name: req.Name, Namespace: req.Namespace}, key); err != nil {
		if errors.IsNotFound(err) {
			// object not found, could have been deleted after
			// reconcile request, hence don't requeue
			return ctrl.Result{}, nil
		}

		return ctrl.Result{}, err
	}

	if !controllerutil.ContainsFinalizer(key, keyFinalizer) {
		l.Info("Adding Finalizer")
		controllerutil.AddFinalizer(key, keyFinalizer)
		return ctrl.Result{}, r.Update(ctx, key)
	}

	if !key.DeletionTimestamp.IsZero() {
		l.Info("Key is being deleted")
		return r.reconcileDelete(ctx, key, true)
	}

	return r.reconcileCreate(ctx, key)
}

func (r *KeyReconciler) reconcileCreate(ctx context.Context, key *b2v1alpha2.Key) (ctrl.Result, error) {
	// Create or update the key
	if err := r.createOrUpdateKey(ctx, key); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *KeyReconciler) createKeySecret(ctx context.Context, key *b2v1alpha2.Key, appkey *backblaze.ApplicationKeyResponse) *corev1.Secret {
	l := log.FromContext(ctx)

	// Secret data
	secretData := map[string][]byte{
		"bucketName": []byte(key.Spec.AtProvider.BucketName),
		"endpoint":   []byte(fmt.Sprintf("s3.%s.backblazeb2.com", string(os.Getenv("B2_REGION")))),
		"keyName":    []byte(appkey.KeyName),
		// AWS S3 compatibile variables
		"AWS_ACCESS_KEY_ID":     []byte(appkey.ApplicationKeyId),
		"AWS_SECRET_ACCESS_KEY": []byte(appkey.ApplicationKey),
	}

	// Check if secret exist
	secret := &corev1.Secret{}
	err := r.Get(context.TODO(), types.NamespacedName{Name: key.Spec.WriteConnectionSecretToRef.Name, Namespace: key.Spec.WriteConnectionSecretToRef.Namespace}, secret)
	if err != nil {
		l.Info("Not found existing secret... creating new")
		secret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      key.Spec.WriteConnectionSecretToRef.Name,
				Namespace: key.Spec.WriteConnectionSecretToRef.Namespace,
			},
			Data: secretData,
		}
	} else {
		l.Info("Found existing secret with credentials, updating values")
		secret.Data = secretData
		err = r.Update(context.TODO(), secret)
		if err != nil {
			l.Error(err, "Unable to update existing secret with credentials")
		}

	}
	return secret
}

func (r *KeyReconciler) createOrUpdateKey(ctx context.Context, key *b2v1alpha2.Key) error {
	l := log.FromContext(ctx)
	l.Info("create or update key")

	// Check if the key exists
	if err := r.Get(ctx, types.NamespacedName{Name: key.Name, Namespace: key.Namespace}, key); err != nil {
		if !errors.IsNotFound(err) {
			return fmt.Errorf("unable to fetch Key: %v", err)
		}
	}

	// Checking if backblaze secrets are set
	if os.Getenv("B2_APPLICATION_ID") == "" || os.Getenv("B2_APPLICATION_KEY") == "" {
		l.Info("B2_APPLICATION_ID or B2_APPLICATION_KEY not set")
	}

	// Initializing backblaze client
	b2, _ := backblaze.NewB2(backblaze.Credentials{
		KeyID:          os.Getenv("B2_APPLICATION_ID"),
		ApplicationKey: os.Getenv("B2_APPLICATION_KEY"),
	})

	// Enable go-backblaze debuging mode (printing all api requests) if B2_DEBUG env is set to "true"
	if os.Getenv("B2_DEBUG") == "true" {
		b2.Debug = true
	}

	// If key was not already reconciled (most likely new CR)
	if !key.Status.Reconciled || key.Status.ToRecreate {
		l.Info("Key is not reconciled")
		var b2_bucket_id string
		var createKeyRequest *backblaze.CreateKeyRequest

		if key.Spec.AtProvider.BucketName != "" || key.Spec.AtProvider.BucketId != "" {
			// If bucketName or bucketId is defined, creating key will defined bucketId
			if key.Spec.AtProvider.BucketName != "" {
				bucket_b2, bucket_b2_err := b2.Bucket(key.Spec.AtProvider.BucketName)
				if bucket_b2_err != nil {
					l.Error(bucket_b2_err, "Failed to find bucket at provider")
				}
				b2_bucket_id = bucket_b2.ID
			} else {
				b2_bucket_id = key.Spec.AtProvider.BucketId
			}

			createKeyRequest = &backblaze.CreateKeyRequest{
				KeyName:      key.Name,
				Capabilities: key.Spec.AtProvider.Capabilities,
				BucketId:     b2_bucket_id,
			}
		} else {
			// If bucketName or bucketId is not defined, creating key that have access to all buckets
			createKeyRequest = &backblaze.CreateKeyRequest{
				KeyName:      key.Name,
				Capabilities: key.Spec.AtProvider.Capabilities,
			}
		}

		// Create application key
		applicationKeyCreate, err := b2.CreateApplicationKey(createKeyRequest)

		if err != nil {
			l.Error(err, "Unable to create application key at provider")
		}

		if applicationKeyCreate.ApplicationKeyId != "" {
			l.Info("Got application key id from provider, creating secret")
			secret := r.createKeySecret(ctx, key, applicationKeyCreate)
			if err := r.Create(ctx, secret); err != nil {
				l.Error(err, "Failed to create new Secret", "Secret.Namespace", secret.Namespace, "Deployment.Name", secret.Name)
			}
		}

		// Saving reconcilation and provider config
		key.Status.Reconciled = true
		key.Status.ToRecreate = false
		key.Status.AtProvider = key.Spec.AtProvider
		key.Status.KeyId = applicationKeyCreate.ApplicationKeyId
		r.Status().Update(ctx, key)
	} else {
		// reconciling loop
		l.Info("Key is reconciled")
		if !reflect.DeepEqual(key.Spec.AtProvider, key.Status.AtProvider) && !key.Status.ToRecreate {
			l.Info("Key resource exist on cluster, updating state")
			// Updating resource at cluster
			key.Status.Reconciled = false
			key.Status.ToRecreate = true
			r.Status().Update(ctx, key)

			// Deleting key
			_, err := r.reconcileDelete(ctx, key, false)
			if err != nil {
				l.Error(err, "Failed to delete secret")
			}

			keyerr := r.createOrUpdateKey(ctx, key)
			if keyerr != nil {
				l.Error(keyerr, "Failed to recreate key")
			}

		}
	}

	return nil
}

func (r *KeyReconciler) reconcileDelete(ctx context.Context, key *b2v1alpha2.Key, deleteSecret bool) (ctrl.Result, error) {

	l := log.FromContext(ctx)
	l.Info("Removing Key")

	if deleteSecret {
		// Retriving existing secret
		existing_secret := &corev1.Secret{}
		if err := r.Client.Get(ctx, types.NamespacedName{
			Name:      key.Spec.WriteConnectionSecretToRef.Name,
			Namespace: key.Spec.WriteConnectionSecretToRef.Namespace},
			existing_secret,
		); err != nil {
			l.Info("Failed to get existing secret at cluster")
		} else {
			// Remove secret
			if err := r.Delete(ctx, existing_secret); err != nil {
				l.Error(err, "Failed to remove secret")
			} else {
				l.Info("Deleted secret at cluster")
			}
		}
	}

	// Checking if backblaze secrets are set
	if os.Getenv("B2_APPLICATION_ID") == "" || os.Getenv("B2_APPLICATION_KEY") == "" {
		l.Info("B2_APPLICATION_ID or B2_APPLICATION_KEY not set")
	}

	// Initializing backblaze client
	b2, _ := backblaze.NewB2(backblaze.Credentials{
		KeyID:          os.Getenv("B2_APPLICATION_ID"),
		ApplicationKey: os.Getenv("B2_APPLICATION_KEY"),
	})

	// Deleting application key on b2
	_, err := b2.DeleteApplicationKey(key.Status.KeyId)
	if err != nil {
		l.Error(err, "Failed to delete application key")
	} else {
		l.Info("Deleted Application Key at provider")
	}

	// Remove the finalizer and update the object
	controllerutil.RemoveFinalizer(key, keyFinalizer)
	if err := r.Update(ctx, key); err != nil {
		return ctrl.Result{}, fmt.Errorf("error removing finalizer: %v", err)
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *KeyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&b2v1alpha2.Key{}).
		Complete(r)
}

## Troubleshooting

### Unable to create public buckets
Before you will be able to create public bucket, you need to enter payment details in your Backblaze account (error: `Account has no payment history. Please make a payment before making a public bucket.`).
```
2024-05-18T16:30:00+02:00	INFO	controllers.Bucket	unable to create bucket, no_payment_history?	{"bucket": "backblaze-operator"}
```

### Unable to create/delete/update bucket
Backblaze have [scheduled maintenance window](https://www.backblaze.com/status/scheduled-maintenance) every Thursday from 11:30 am to 1:30 pm Pacific Time (6:30 pm - 8:30 pm UTC). During this time, creating, updating or deleting buckets might not work, but the operator should periodicly retry operation on failure.

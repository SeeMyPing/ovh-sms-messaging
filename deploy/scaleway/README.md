# Déploiement Scaleway (Terraform)

Crée toute l'infrastructure décrite dans le [README principal](../../README.md) :

| Ressource | Rôle |
|---|---|
| `scaleway_mnq_sqs` | Activation de Scaleway Queues sur le projet |
| `scaleway_mnq_sqs_credentials` ×3 | `terraform` (gestion des queues), `trigger` (lecture), `producer` (publication) |
| `scaleway_mnq_sqs_queue` ×2 | Queue principale + DLQ (`max_receive_count` = 4) |
| `scaleway_container_namespace`, `scaleway_container` | Conteneur **privé**, `min_scale` 0, mot de passe ou clé API en secret |
| `scaleway_container_trigger` | Trigger SQS → `POST /` sur le conteneur |

## Prérequis

- Terraform ≥ 1.5 (ou OpenTofu)
- Identifiants Scaleway dans l'environnement :
  ```sh
  export SCW_ACCESS_KEY=... SCW_SECRET_KEY=... SCW_DEFAULT_PROJECT_ID=...
  ```
- Un compte chez un fournisseur SMS : accès SMPP (adresse du SMSC, `system_id`, mot de passe)
  ou API Twilio, OVH (http2sms) ou ClickSend, et un expéditeur autorisé.

## Déploiement

L'image est construite et publiée sur `ghcr.io/seemyping/sqs-to-smpp-gateway` par la CI à
chaque merge sur `main` (voir le [README principal](../../README.md#ci)). Choisir le tag
`sha-<commit>` à déployer dans les packages GitHub du dépôt.

```sh
cd deploy/scaleway
cp terraform.tfvars.example terraform.tfvars   # à compléter, dont image_tag
export TF_VAR_smpp_password='...'   # ou TF_VAR_twilio_auth_token, TF_VAR_ovh_sms_password, TF_VAR_clicksend_api_key

terraform init
terraform apply
```

Pour les versions suivantes : changer `image_tag`, puis `terraform apply`. Utiliser les tags
`sha-…` (ou de version) plutôt que `latest` : réutiliser un tag ne redéploie pas le conteneur.

**IP sortante** : un Serverless Container n'a pas d'IP de sortie fixe. Si le fournisseur
filtre par IP (souvent en SMPP, en option chez OVH), l'envoi échoue : les messages sont
réessayés puis conservés en DLQ.

Le package ghcr.io doit être **public** pour que Scaleway puisse tirer l'image sans
identifiants (Package settings → Change visibility).

Committer le `.terraform.lock.hcl` créé par `terraform init`. Pour qu'il fonctionne sur
tous les postes : `terraform providers lock -platform=linux_amd64 -platform=darwin_arm64`.

## Envoyer un SMS

Les producteurs utilisent les identifiants `producer`, qui ne peuvent que publier :

```sh
export AWS_ACCESS_KEY_ID=$(terraform output -raw producer_access_key)
export AWS_SECRET_ACCESS_KEY=$(terraform output -raw producer_secret_key)

aws sqs send-message --region fr-par \
  --endpoint-url "$(terraform output -raw sqs_endpoint)" \
  --queue-url "$(terraform output -raw queue_url)" \
  --message-body '{"to":"+33612345678","message":"Hello"}'
```

## Variables principales

| Variable | Défaut | Description |
|---|---|---|
| `image_tag` | — | Tag de l'image à déployer (`sha-<commit>`) |
| `image` | `ghcr.io/seemyping/sqs-to-smpp-gateway` | Image sans tag |
| `sms_protocol` | `smpp` | `smpp` ou `http` |
| `sms_provider` | vide | En `http` : `twilio`, `ovh` ou `clicksend` |
| `sms_sender` | vide | Expéditeur par défaut |
| `smpp_addr`, `smpp_system_id` | vide | SMSC (`hôte:port`) et identifiant, en `smpp` |
| `smpp_password` | vide | Sensible : passer par `TF_VAR_smpp_password` |
| `smpp_tls` | `false` | Connexion TLS |
| `smpp_system_type` | vide | Si le fournisseur en demande un |
| `twilio_account_sid`, `twilio_messaging_service_sid` | vide | Twilio |
| `twilio_auth_token` | vide | Sensible : `TF_VAR_twilio_auth_token` |
| `ovh_sms_account`, `ovh_sms_login`, `ovh_sms_no_stop` | vide | OVH http2sms |
| `ovh_sms_password` | vide | Sensible : `TF_VAR_ovh_sms_password` |
| `clicksend_username` | vide | ClickSend |
| `clicksend_api_key` | vide | Sensible : `TF_VAR_clicksend_api_key` |
| `region` | `fr-par` | Région Scaleway |
| `name` | `sqs-to-smpp` | Préfixe des ressources |
| `max_scale` | `1` | Instances max : en SMPP, une session (bind) par instance |
| `container_timeout` | `30` | Timeout d'une requête (s) |
| `visibility_timeout_seconds` | `60` | Doit rester > `container_timeout` (vérifié au plan) |
| `message_max_age` | 4 jours | Rétention de la queue |
| `dlq_message_max_age` | 14 jours | Rétention de la DLQ |
| `log_level` | `info` | Passer à `debug` pour voir les headers du trigger |

Seules les variables non vides sont passées au conteneur, qui vérifie au démarrage que
celles du protocole et du fournisseur choisis sont présentes. La liste complète est dans
[`variables.tf`](variables.tf).

## Sécurité

Les mots de passe et clés API du fournisseur et les clés SQS sont stockés **en clair dans le state** Terraform.
Utiliser un backend distant chiffré et à accès restreint (par exemple un bucket Scaleway
Object Storage privé), jamais un state committé.

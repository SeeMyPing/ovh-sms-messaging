# sqs-to-smpp-gateway

Envoie en SMS, via le protocole **SMPP 3.4**, les messages déposés dans une queue.
L'application ne dépend d'aucun fournisseur SMS : elle parle à n'importe quel SMSC SMPP.

Elle reçoit les messages de la queue en `POST` HTTP. Le déploiement fourni utilise
[Scaleway Queues](https://www.scaleway.com/en/docs/queues/) et un **trigger** qui pousse
chaque message vers un **Serverless Container** (voir [`deploy/`](deploy)).

```
Producteur ──▶ Queue ──Trigger──▶ POST / ──▶ Conteneur ──SMPP──▶ SMSC
                 │
                 └──▶ DLQ
```

## Format du message

```json
{
  "to": "+33612345678",
  "message": "Votre code est 123456",
  "sender": "MYAPP"
}
```

| Champ | Obligatoire | Description |
|---|---|---|
| `to` | oui | **Un seul** destinataire, au format international (`+33…` ou `0033…`, espaces, points et tirets tolérés). Les numéros nationaux (`06…`) sont refusés. |
| `message` | oui | Texte du SMS, 1600 caractères max. |
| `sender` | non | Expéditeur : alphanumérique (11 caractères ASCII max), numéro court ou numéro international `+…`. Par défaut : `SMPP_SOURCE_ADDR`. Le fournisseur peut n'accepter que certains expéditeurs. |

Pour envoyer à plusieurs destinataires, déposer un message par destinataire.

### Encodage et messages longs

- **GSM 03.38** (`data_coding` 0) si tous les caractères en font partie : 160 caractères par SMS.
- **UCS-2** (`data_coding` 8) sinon (accents hors GSM, emojis, alphabets non latins) : 70 caractères par SMS.
- Au-delà, le message part en plusieurs SMS concaténés (UDH, `esm_class` 0x40) :
  153 caractères GSM ou 67 UCS-2 par partie. Un caractère n'est jamais coupé entre deux parties.

## Réponses au trigger

Le trigger supprime le message sur une réponse 2xx et le réessaie (3 fois max) sinon.

| Cas | Réponse | Effet |
|---|---|---|
| SMS accepté par le SMSC (`submit_sm_resp` OK) | `200` | Message supprimé |
| Message invalide : JSON, numéro, longueur, expéditeur | `200` | Message supprimé, erreur loguée |
| Refusé par le SMSC pour le message lui-même : `ESME_RINVDSTADR`, `ESME_RINVDSTTON`, `ESME_RINVDSTNPI`, `ESME_RINVMSGLEN` | `200` | Message supprimé, erreur loguée |
| Tout le reste : réseau, timeout, bind refusé (identifiants, IP), `ESME_RTHROTTLED`, `ESME_RMSGQFUL`, `ESME_RSYSERR`, expéditeur refusé… | `503` | Réessayé, puis DLQ |

Les erreurs définitives répondent `200` exprès : les réessayer échouerait de la même façon.
Les erreurs qui dépendent de la configuration (identifiants, IP autorisées, expéditeur)
sont traitées comme temporaires : les messages restent récupérables dans la DLQ.

« Accepté par le SMSC » ne veut pas dire « livré » : les accusés de réception (DLR)
ne sont pas gérés.

## Session SMPP

- Bind **transmitter** au premier message, puis session conservée tant que l'instance vit
  et partagée entre les requêtes concurrentes.
- `enquire_link` périodique ; rebind automatique si la connexion tombe.
- `unbind` à l'arrêt (SIGTERM), après la fin des envois en cours.
- Chaque instance ouvre sa propre session : le nombre d'instances ne doit pas dépasser
  le nombre de binds autorisés par le fournisseur.

## Configuration

| Variable | Obligatoire | Défaut | Description |
|---|---|---|---|
| `SMPP_ADDR` | oui | | Adresse du SMSC, `hôte:port` |
| `SMPP_SYSTEM_ID` | oui | | Identifiant SMPP |
| `SMPP_PASSWORD` | oui | | Mot de passe SMPP — **à déclarer en secret** |
| `SMPP_SOURCE_ADDR` | oui | | Expéditeur par défaut (mêmes règles que `sender`) |
| `SMPP_SYSTEM_TYPE` | non | vide | `system_type`, si le fournisseur en demande un |
| `SMPP_TLS` | non | `false` | Connexion TLS au SMSC |
| `SMPP_CONNECT_TIMEOUT` | non | `10s` | Connexion TCP et bind |
| `SMPP_SUBMIT_TIMEOUT` | non | `10s` | Attente de chaque `submit_sm_resp` |
| `SMPP_ENQUIRE_LINK` | non | `30s` | Période du keep-alive |
| `LOG_LEVEL` | non | `info` | `debug`, `info`, `warn`, `error` |
| `PORT` | non | `8080` | Port HTTP |

Les logs sont en JSON sur la sortie standard. Les numéros y sont masqués et le mot de
passe n'y apparaît jamais. En `debug`, les headers HTTP reçus sont logués (hors credentials).

## Développement

Avec le simulateur [smscsim](https://github.com/ukarim/smscsim) :

```sh
go test ./...

docker run -d -p 2775:2775 -p 12775:12775 ukarim/smscsim

SMPP_ADDR=localhost:2775 SMPP_SYSTEM_ID=test SMPP_PASSWORD=test SMPP_SOURCE_ADDR=MYAPP \
  go run ./cmd/sqs-to-smpp-gateway

curl -i -X POST localhost:8080/ -d '{"to":"+33612345678","message":"test"}'
curl -i localhost:8080/healthz
```

Les tests du client SMPP tournent contre un faux SMSC embarqué
([`internal/smpp/fake_smsc_test.go`](internal/smpp/fake_smsc_test.go)).

## Déploiement

| Cible | Dossier |
|---|---|
| Scaleway (Queues + Serverless Containers) | [`deploy/scaleway`](deploy/scaleway) |

L'image est publiée par la [CI](#ci) sur `ghcr.io/seemyping/sqs-to-smpp-gateway`
(`linux/amd64`).

**IP sortante** : beaucoup de fournisseurs SMPP n'acceptent que des IP déclarées.
Une plateforme serverless n'a en général pas d'IP de sortie fixe : dans ce cas le bind
échoue, les messages sont réessayés puis conservés en DLQ.

## CI

Le workflow [`.github/workflows/ci.yml`](.github/workflows/ci.yml) lance trois jobs :

| Job | Contenu |
|---|---|
| `Go` | `gofmt`, `go vet`, `go test -race` |
| `Terraform` | `terraform fmt -check`, `terraform validate` sur `deploy/scaleway` |
| `Image` | Build de l'image `linux/amd64` ; publiée sur ghcr.io hors pull requests |

Tags publiés :

| Événement | Tags |
|---|---|
| Pull request | aucun (build seul) |
| Merge sur `main` | `sha-<commit>`, `latest` |
| Tag `vX.Y.Z` | `X.Y.Z`, `sha-<commit>` |

### Protection de `main`

[`.github/rulesets/main.json`](.github/rulesets/main.json) définit un ruleset GitHub :
pull request obligatoire (sans approbation requise, conversations résolues), jobs `Go`,
`Terraform` et `Image` verts sur une branche à jour, ni force-push ni suppression.

Les rulesets ne sont pas appliqués depuis le dépôt : il faut les importer une fois.

- Interface : *Settings → Rules → Rulesets → New ruleset → Import a ruleset*, choisir le fichier.
- CLI :
  ```sh
  gh api -X POST repos/SeeMyPing/sqs-to-smpp-gateway/rulesets --input .github/rulesets/main.json
  ```

Après une modification du fichier, mettre à jour le ruleset existant
(`gh api -X PUT repos/SeeMyPing/sqs-to-smpp-gateway/rulesets/<id> --input …`).

## Limites connues

- **SMS en double possibles** : la livraison est « au moins une fois ». Si le SMSC accepte
  le SMS mais que la réponse se perd (timeout, arrêt du conteneur), le trigger rejoue le
  message. Pour un message long, un échec sur une partie fait renvoyer toutes les parties.
- **Pas de DLR** : les accusés de réception ne sont pas remontés.

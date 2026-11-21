# Catalog legacy adapter

`control/catalog/models.ProviderSet` is this adapter's production caller. The adapter reuses the v1 plaza, the finite gateway model visibility resolver, project resource mappings, legacy account discovery, connections verification and gateway usage/ops records. It supplies typed catalog acquisitions. The modules/catalog projector and its PostgreSQL writer own public redaction and publication.

pricing replaces legacy display price conversion when the exact price reader is composed. routing supplies effective route eligibility, and catalog-verification extends acquisition with measured action verification. retirement can remove this adapter once those readers and the legacy plaza share the resulting catalog projection.

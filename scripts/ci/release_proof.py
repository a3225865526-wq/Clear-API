"""Shared release-mode contract; evidence validation never grants host access."""

PG03_GATES = frozenset({
    'runtime_integrity', 'billing', 'backup_restore', 'executor_pairing',
    'sqlite_upgrade_idempotency', 'mysql_upgrade_idempotency',
    'postgres_upgrade_idempotency', 'gateway_executor_provider_usage_billing',
    'replay_and_duplicate_charge', 'state_isolation', 'safe_rollback',
})
OFF_GATES = (PG03_GATES - {'executor_pairing', 'gateway_executor_provider_usage_billing'}
             | {'pool_disabled', 'ordinary_provider_usage_billing'})


def required_gates(pool_mode, release_stage='commercial'):
    if pool_mode not in ('off', 'pg03') or release_stage not in ('commercial', 'controlled'):
        raise ValueError('unsupported release mode or stage')
    if pool_mode == 'off':
        if release_stage != 'commercial':
            raise ValueError('off release cannot defer ordinary Provider acceptance')
        return OFF_GATES
    return (PG03_GATES - {'gateway_executor_provider_usage_billing'}
            if release_stage == 'controlled' else PG03_GATES)


def proof_mode(proof, controlled=False):
    legacy = 'clearapi.controlled-release/v1' if controlled else 'clearapi.automatic-release/v1'
    if proof.get('schema') == legacy:
        # Existing v1 artifacts describe PG03 only; never reinterpret them as off.
        if proof.get('pool_mode', 'pg03') != 'pg03':
            raise ValueError('legacy proof cannot authorize off')
        return 'pg03'
    if not controlled and proof.get('schema') == 'clearapi.automatic-release/v2':
        mode = proof.get('pool_mode')
        required_gates(mode)
        return mode
    raise ValueError('unsupported proof contract')


def validate_receipt_mode(receipt, pool_mode):
    if pool_mode == 'off':
        if receipt.get('schema') != 'clearapi.gate-evidence/v2' or receipt.get('pool_mode') != 'off':
            raise ValueError('off receipt mode/schema mismatch')
    elif (receipt.get('schema') != 'clearapi.gate-evidence/v1'
          or receipt.get('pool_mode', 'pg03') != 'pg03'):
        raise ValueError('PG03 receipt mode/schema mismatch')

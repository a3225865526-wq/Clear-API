--
-- PostgreSQL database dump
--


-- Dumped from database version 17.6
-- Dumped by pg_dump version 17.6

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET transaction_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

SET default_tablespace = '';

SET default_table_access_method = heap;

--
-- Name: abilities; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.abilities (
    "group" character varying(64) NOT NULL,
    model character varying(255) NOT NULL,
    channel_id bigint NOT NULL,
    enabled boolean,
    priority bigint DEFAULT 0,
    weight bigint DEFAULT 0,
    tag text
);


--
-- Name: audit_logs; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.audit_logs (
    id bigint NOT NULL,
    event_id character varying(64),
    user_id bigint,
    username character varying(64),
    actor_role bigint,
    created_at bigint,
    category character varying(24),
    action character varying(128),
    token_ref character varying(64),
    auth_method character varying(24),
    ip character varying(64),
    user_agent character varying(512),
    method character varying(16),
    route character varying(255),
    status bigint,
    success boolean,
    request_id character varying(64),
    content text,
    other json
);


--
-- Name: audit_logs_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.audit_logs_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: audit_logs_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.audit_logs_id_seq OWNED BY public.audit_logs.id;


--
-- Name: auth_flows; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.auth_flows (
    id bigint NOT NULL,
    token_hash character(64) NOT NULL,
    purpose character varying(32) NOT NULL,
    provider character varying(64),
    intent character varying(16),
    user_id bigint,
    session_id character varying(64),
    payload text,
    created_at timestamp with time zone,
    expires_at timestamp with time zone NOT NULL,
    consumed_at timestamp with time zone
);


--
-- Name: auth_flows_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.auth_flows_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: auth_flows_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.auth_flows_id_seq OWNED BY public.auth_flows.id;


--
-- Name: authz_roles; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.authz_roles (
    id bigint NOT NULL,
    key character varying(64) NOT NULL,
    name character varying(100) NOT NULL,
    description text,
    built_in boolean,
    enabled boolean,
    sort bigint,
    created_at bigint,
    updated_at bigint
);


--
-- Name: authz_roles_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.authz_roles_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: authz_roles_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.authz_roles_id_seq OWNED BY public.authz_roles.id;


--
-- Name: balance_transactions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.balance_transactions (
    id bigint NOT NULL,
    user_id bigint,
    created_at bigint,
    type character varying(16),
    amount_quota bigint,
    balance_before bigint,
    balance_after bigint,
    recharge_cents bigint DEFAULT 0,
    reference_type character varying(16) DEFAULT ''::character varying,
    reference_id bigint DEFAULT 0,
    description character varying(255) DEFAULT ''::character varying,
    metadata text DEFAULT ''::text
);


--
-- Name: balance_transactions_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.balance_transactions_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: balance_transactions_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.balance_transactions_id_seq OWNED BY public.balance_transactions.id;


--
-- Name: billing_auth_snapshots; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.billing_auth_snapshots (
    id bigint NOT NULL,
    request_id character varying(128),
    policy_version bigint,
    mode character varying(16),
    estimator_version character varying(32),
    profile_source character varying(255),
    profile_version bigint,
    profile_approved boolean,
    cache_mode character varying(16),
    pricing_mode character varying(24),
    pricing_fingerprint character varying(80),
    group_ratio numeric,
    quota_per_unit numeric,
    margin numeric,
    floor_quota bigint,
    visible_input_max bigint,
    hidden_input_max bigint,
    hidden_cache_read_max bigint,
    hidden_cache_write_max bigint,
    hidden_cache_write1h_max bigint,
    output_max bigint,
    context_len_max bigint,
    required_quota bigint,
    cap_quota bigint,
    reserved_quota bigint,
    updated_at bigint
);


--
-- Name: billing_auth_snapshots_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.billing_auth_snapshots_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: billing_auth_snapshots_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.billing_auth_snapshots_id_seq OWNED BY public.billing_auth_snapshots.id;


--
-- Name: billing_completion_outboxes; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.billing_completion_outboxes (
    id bigint NOT NULL,
    request_id character varying(128),
    payload text,
    state character varying(16),
    attempts bigint,
    next_attempt bigint
);


--
-- Name: billing_completion_outboxes_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.billing_completion_outboxes_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: billing_completion_outboxes_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.billing_completion_outboxes_id_seq OWNED BY public.billing_completion_outboxes.id;


--
-- Name: billing_completion_receipts; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.billing_completion_receipts (
    id bigint NOT NULL,
    request_id character varying(128)
);


--
-- Name: billing_completion_receipts_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.billing_completion_receipts_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: billing_completion_receipts_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.billing_completion_receipts_id_seq OWNED BY public.billing_completion_receipts.id;


--
-- Name: billing_reservation_decisions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.billing_reservation_decisions (
    id bigint NOT NULL,
    reservation_id bigint,
    user_id bigint,
    original_state character varying(24),
    reserved bigint,
    target bigint,
    applied bigint,
    refund_quota bigint,
    final_quota bigint,
    reason character varying(500),
    token_refund character varying(24),
    actor_id bigint,
    created_at bigint
);


--
-- Name: billing_reservation_decisions_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.billing_reservation_decisions_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: billing_reservation_decisions_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.billing_reservation_decisions_id_seq OWNED BY public.billing_reservation_decisions.id;


--
-- Name: billing_reservations; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.billing_reservations (
    id bigint NOT NULL,
    request_id character varying(128),
    user_id bigint,
    token_id bigint,
    reserved bigint,
    target bigint,
    applied bigint,
    state character varying(24),
    updated_at bigint
);


--
-- Name: billing_reservations_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.billing_reservations_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: billing_reservations_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.billing_reservations_id_seq OWNED BY public.billing_reservations.id;


--
-- Name: billing_review_markers; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.billing_review_markers (
    id bigint NOT NULL,
    reservation_id bigint,
    reason character varying(32),
    created_at bigint
);


--
-- Name: billing_review_markers_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.billing_review_markers_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: billing_review_markers_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.billing_review_markers_id_seq OWNED BY public.billing_review_markers.id;


--
-- Name: casbin_rule; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.casbin_rule (
    id bigint NOT NULL,
    ptype character varying(100),
    v0 character varying(100),
    v1 character varying(100),
    v2 character varying(100),
    v3 character varying(100),
    v4 character varying(100),
    v5 character varying(100)
);


--
-- Name: casbin_rule_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.casbin_rule_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: casbin_rule_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.casbin_rule_id_seq OWNED BY public.casbin_rule.id;


--
-- Name: channels; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.channels (
    id bigint NOT NULL,
    type bigint DEFAULT 0,
    key text NOT NULL,
    open_ai_organization text,
    test_model text,
    status bigint DEFAULT 1,
    name text,
    weight bigint DEFAULT 0,
    created_time bigint,
    test_time bigint,
    response_time bigint,
    base_url text DEFAULT ''::text,
    other text,
    balance numeric,
    balance_updated_time bigint,
    models text,
    "group" character varying(64) DEFAULT 'default'::character varying,
    used_quota bigint DEFAULT 0,
    model_mapping text,
    status_code_mapping character varying(1024) DEFAULT ''::character varying,
    priority bigint DEFAULT 0,
    auto_ban bigint DEFAULT 1,
    other_info text,
    tag text,
    setting text,
    param_override text,
    header_override text,
    remark character varying(255),
    channel_info json,
    settings text
);


--
-- Name: channels_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.channels_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: channels_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.channels_id_seq OWNED BY public.channels.id;


--
-- Name: checkins; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.checkins (
    id bigint NOT NULL,
    user_id bigint NOT NULL,
    checkin_date character varying(10) NOT NULL,
    quota_awarded bigint NOT NULL,
    created_at bigint
);


--
-- Name: checkins_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.checkins_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: checkins_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.checkins_id_seq OWNED BY public.checkins.id;


--
-- Name: console_monitor_attempts; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.console_monitor_attempts (
    id bigint NOT NULL,
    monitor_id bigint,
    started_at bigint,
    lease_until bigint,
    finished_at bigint,
    reserved_cost_micros bigint,
    failure boolean,
    cleanup_state character varying(32),
    cleanup_started_at bigint,
    recovery_reason character varying(512),
    recovered_at bigint
);


--
-- Name: console_monitor_attempts_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.console_monitor_attempts_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: console_monitor_attempts_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.console_monitor_attempts_id_seq OWNED BY public.console_monitor_attempts.id;


--
-- Name: console_monitor_policies; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.console_monitor_policies (
    id bigint NOT NULL,
    schema_version bigint,
    enabled boolean,
    max_concurrent bigint,
    daily_requests bigint,
    daily_cost_micros bigint,
    retention_days bigint
);


--
-- Name: console_monitors; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.console_monitors (
    id bigint NOT NULL,
    channel_id bigint,
    display_name text,
    platform text,
    model text,
    published boolean DEFAULT false,
    identity character varying(64),
    target_kind character varying(32),
    target_ref character varying(255),
    protocol character varying(32),
    enabled boolean,
    archived boolean,
    interval_seconds bigint,
    timeout_seconds bigint,
    stale_seconds bigint,
    daily_requests bigint,
    request_cost_micros bigint,
    allowlisted boolean,
    created_by bigint,
    created_at bigint
);


--
-- Name: console_monitors_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.console_monitors_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: console_monitors_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.console_monitors_id_seq OWNED BY public.console_monitors.id;


--
-- Name: console_probes; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.console_probes (
    id bigint NOT NULL,
    monitor_id bigint,
    checked_at bigint,
    status text,
    latency_ms bigint,
    endpoint_ms bigint,
    error_code text,
    source_channel_id bigint,
    source_kind character varying(32)
);


--
-- Name: console_probes_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.console_probes_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: console_probes_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.console_probes_id_seq OWNED BY public.console_probes.id;


--
-- Name: credit_approval_events; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.credit_approval_events (
    id bigint NOT NULL,
    product_id character varying(64),
    revision bigint,
    digest character varying(64),
    policy character varying(64),
    edited_by bigint,
    actor_id bigint,
    decision character varying(24),
    created_time bigint,
    evidence_batch_id bigint,
    delivery_evidence character varying(256),
    solo_operator boolean,
    reference_approval_id bigint
);


--
-- Name: credit_approval_events_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.credit_approval_events_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: credit_approval_events_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.credit_approval_events_id_seq OWNED BY public.credit_approval_events.id;


--
-- Name: custom_oauth_providers; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.custom_oauth_providers (
    id bigint NOT NULL,
    name character varying(64) NOT NULL,
    slug character varying(64) NOT NULL,
    icon character varying(128) DEFAULT ''::character varying,
    enabled boolean DEFAULT false,
    client_id character varying(256),
    client_secret character varying(512),
    authorization_endpoint character varying(512),
    token_endpoint character varying(512),
    user_info_endpoint character varying(512),
    scopes character varying(256) DEFAULT 'openid profile email'::character varying,
    user_id_field character varying(128) DEFAULT 'sub'::character varying,
    username_field character varying(128) DEFAULT 'preferred_username'::character varying,
    display_name_field character varying(128) DEFAULT 'name'::character varying,
    email_field character varying(128) DEFAULT 'email'::character varying,
    well_known character varying(512),
    auth_style bigint DEFAULT 0,
    access_policy text,
    access_denied_message character varying(512),
    created_at timestamp with time zone,
    updated_at timestamp with time zone
);


--
-- Name: custom_oauth_providers_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.custom_oauth_providers_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: custom_oauth_providers_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.custom_oauth_providers_id_seq OWNED BY public.custom_oauth_providers.id;


--
-- Name: external_identity_claims; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.external_identity_claims (
    id bigint NOT NULL,
    provider character varying(32) NOT NULL,
    subject character varying(128) NOT NULL,
    user_id bigint NOT NULL,
    created_at timestamp with time zone
);


--
-- Name: external_identity_claims_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.external_identity_claims_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: external_identity_claims_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.external_identity_claims_id_seq OWNED BY public.external_identity_claims.id;


--
-- Name: finance_audits; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.finance_audits (
    id bigint NOT NULL,
    actor_id bigint,
    action character varying(32),
    entity_id bigint,
    previous_id bigint,
    request_id character varying(64),
    created_at bigint
);


--
-- Name: finance_audits_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.finance_audits_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: finance_audits_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.finance_audits_id_seq OWNED BY public.finance_audits.id;


--
-- Name: finance_cash_receipts; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.finance_cash_receipts (
    id bigint NOT NULL,
    order_id bigint,
    amount_cny character varying(64),
    paid_at bigint,
    quota_per_code bigint,
    code_count bigint,
    redemption_ids text,
    evidence character varying(500),
    created_by bigint,
    created_at bigint
);


--
-- Name: finance_cash_receipts_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.finance_cash_receipts_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: finance_cash_receipts_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.finance_cash_receipts_id_seq OWNED BY public.finance_cash_receipts.id;


--
-- Name: finance_cash_refunds; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.finance_cash_refunds (
    id bigint NOT NULL,
    event_key character varying(100),
    receipt_id bigint,
    order_id bigint,
    state character varying(16),
    amount_cny character varying(64),
    refunded_at bigint,
    evidence character varying(500),
    created_by bigint,
    created_at bigint
);


--
-- Name: finance_cash_refunds_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.finance_cash_refunds_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: finance_cash_refunds_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.finance_cash_refunds_id_seq OWNED BY public.finance_cash_refunds.id;


--
-- Name: finance_expenses; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.finance_expenses (
    id bigint NOT NULL,
    event_key character varying(100),
    ledger character varying(16),
    category character varying(100),
    pool_key character varying(100),
    account_ref character varying(100),
    amount character varying(64),
    currency character varying(3),
    fx_version_id bigint,
    period_from bigint,
    period_to bigint,
    recurrence character varying(16),
    adjusts_id bigint,
    source character varying(500),
    note character varying(1000),
    created_at bigint,
    created_by bigint
);


--
-- Name: finance_expenses_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.finance_expenses_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: finance_expenses_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.finance_expenses_id_seq OWNED BY public.finance_expenses.id;


--
-- Name: finance_rebuild_snapshots; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.finance_rebuild_snapshots (
    id bigint NOT NULL,
    user_id bigint,
    fact_count bigint,
    facts text,
    before text,
    hash character varying(64),
    evidence character varying(500),
    created_by bigint,
    created_at bigint
);


--
-- Name: finance_rebuild_snapshots_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.finance_rebuild_snapshots_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: finance_rebuild_snapshots_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.finance_rebuild_snapshots_id_seq OWNED BY public.finance_rebuild_snapshots.id;


--
-- Name: finance_request_events; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.finance_request_events (
    id bigint NOT NULL,
    event_key character varying(100),
    request_id character varying(64),
    attempt_id character varying(64),
    phase character varying(32),
    user_id bigint,
    channel_id bigint,
    upstream_model character varying(200),
    at bigint,
    payload text
);


--
-- Name: finance_request_events_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.finance_request_events_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: finance_request_events_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.finance_request_events_id_seq OWNED BY public.finance_request_events.id;


--
-- Name: finance_revenue_allocations; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.finance_revenue_allocations (
    id bigint NOT NULL,
    event_id bigint,
    lot_id bigint,
    quota bigint,
    value_cny character varying(64)
);


--
-- Name: finance_revenue_allocations_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.finance_revenue_allocations_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: finance_revenue_allocations_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.finance_revenue_allocations_id_seq OWNED BY public.finance_revenue_allocations.id;


--
-- Name: finance_revenue_lots; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.finance_revenue_lots (
    id bigint NOT NULL,
    user_id bigint,
    event_id bigint,
    lot_key character varying(100),
    source character varying(16),
    total_quota bigint,
    used_quota bigint,
    value_cny character varying(64),
    recognized_cny character varying(64)
);


--
-- Name: finance_revenue_lots_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.finance_revenue_lots_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: finance_revenue_lots_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.finance_revenue_lots_id_seq OWNED BY public.finance_revenue_lots.id;


--
-- Name: finance_revenue_records; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.finance_revenue_records (
    id bigint NOT NULL,
    user_id bigint,
    event_key character varying(100),
    revision bigint,
    at bigint,
    kind character varying(16),
    quota bigint,
    source character varying(16),
    value_cny character varying(64),
    reverses bigint,
    evidence character varying(500),
    created_at bigint,
    created_by bigint
);


--
-- Name: finance_revenue_records_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.finance_revenue_records_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: finance_revenue_records_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.finance_revenue_records_id_seq OWNED BY public.finance_revenue_records.id;


--
-- Name: finance_schemas; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.finance_schemas (
    id bigint NOT NULL,
    version bigint
);


--
-- Name: finance_supplier_bill_lines; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.finance_supplier_bill_lines (
    id bigint NOT NULL,
    event_key character varying(100),
    bill_id bigint,
    channel_id bigint,
    category character varying(32),
    upstream_model character varying(200),
    reference character varying(100),
    usage_from bigint,
    usage_to bigint,
    quantity character varying(64),
    currency character varying(3),
    amount character varying(64),
    fx_version_id bigint,
    amount_cny character varying(64),
    replaces_line_id bigint,
    references_line_id bigint,
    evidence character varying(500),
    created_by bigint,
    created_at bigint
);


--
-- Name: finance_supplier_bill_lines_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.finance_supplier_bill_lines_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: finance_supplier_bill_lines_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.finance_supplier_bill_lines_id_seq OWNED BY public.finance_supplier_bill_lines.id;


--
-- Name: finance_supplier_bills; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.finance_supplier_bills (
    id bigint NOT NULL,
    event_key character varying(100),
    channel_id bigint,
    scope character varying(32),
    revision bigint,
    replaces_id bigint,
    period_from bigint,
    period_to bigint,
    currency character varying(3),
    amount character varying(64),
    fx_version_id bigint,
    amount_cny character varying(64),
    evidence character varying(500),
    created_by bigint,
    created_at bigint
);


--
-- Name: finance_supplier_bills_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.finance_supplier_bills_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: finance_supplier_bills_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.finance_supplier_bills_id_seq OWNED BY public.finance_supplier_bills.id;


--
-- Name: finance_sync_events; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.finance_sync_events (
    id bigint NOT NULL,
    event_key character varying(100),
    topic character varying(32),
    user_id bigint,
    ref_id bigint,
    at bigint,
    payload text,
    status character varying(16),
    attempts bigint,
    next_attempt_at bigint,
    error_class character varying(32),
    claimed_at bigint,
    processed_at bigint,
    created_at bigint
);


--
-- Name: finance_sync_events_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.finance_sync_events_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: finance_sync_events_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.finance_sync_events_id_seq OWNED BY public.finance_sync_events.id;


--
-- Name: finance_versions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.finance_versions (
    id bigint NOT NULL,
    kind character varying(16),
    record_key character varying(100),
    revision bigint,
    parent_id bigint,
    effective_from bigint,
    effective_to bigint,
    active boolean,
    payload text,
    source character varying(500),
    created_at bigint,
    created_by bigint
);


--
-- Name: finance_versions_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.finance_versions_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: finance_versions_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.finance_versions_id_seq OWNED BY public.finance_versions.id;


--
-- Name: finance_wallets; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.finance_wallets (
    user_id bigint NOT NULL,
    revision bigint,
    last_at bigint,
    remaining_quota bigint,
    known_backing_cny character varying(64),
    known_revenue_cny character varying(64),
    unknown_credit_count bigint,
    unknown_consumed_quota bigint
);


--
-- Name: login_encryption_keys; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.login_encryption_keys (
    id bigint NOT NULL,
    slot character varying(32) NOT NULL,
    private_key_pem text NOT NULL
);


--
-- Name: login_encryption_keys_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.login_encryption_keys_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: login_encryption_keys_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.login_encryption_keys_id_seq OWNED BY public.login_encryption_keys.id;


--
-- Name: logs; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.logs (
    id bigint NOT NULL,
    user_id bigint,
    created_at bigint,
    type bigint,
    content text,
    username text DEFAULT ''::text,
    token_name text DEFAULT ''::text,
    model_name text DEFAULT ''::text,
    quota bigint DEFAULT 0,
    prompt_tokens bigint DEFAULT 0,
    completion_tokens bigint DEFAULT 0,
    use_time bigint DEFAULT 0,
    is_stream boolean,
    channel_id bigint,
    channel_name text,
    token_id bigint DEFAULT 0,
    "group" text,
    ip text DEFAULT ''::text,
    request_id character varying(64) DEFAULT ''::character varying,
    upstream_request_id character varying(128) DEFAULT ''::character varying,
    other text
);


--
-- Name: logs_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.logs_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: logs_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.logs_id_seq OWNED BY public.logs.id;


--
-- Name: midjourneys; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.midjourneys (
    id bigint NOT NULL,
    code bigint,
    user_id bigint,
    action character varying(40),
    mj_id text,
    prompt text,
    prompt_en text,
    description text,
    state text,
    submit_time bigint,
    start_time bigint,
    finish_time bigint,
    image_url text,
    video_url text,
    video_urls text,
    status character varying(20),
    progress character varying(30),
    fail_reason text,
    channel_id bigint,
    quota bigint,
    buttons text,
    properties text,
    token_id bigint DEFAULT 0,
    billing_channel_id bigint DEFAULT 0
);


--
-- Name: midjourneys_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.midjourneys_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: midjourneys_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.midjourneys_id_seq OWNED BY public.midjourneys.id;


--
-- Name: models; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.models (
    id bigint NOT NULL,
    model_name character varying(128) NOT NULL,
    description text,
    icon character varying(128),
    tags character varying(255),
    vendor_id bigint,
    endpoints text,
    status bigint DEFAULT 1,
    sync_official bigint DEFAULT 1,
    created_time bigint,
    updated_time bigint,
    deleted_at timestamp with time zone,
    name_rule bigint DEFAULT 0
);


--
-- Name: models_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.models_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: models_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.models_id_seq OWNED BY public.models.id;


--
-- Name: offline_order_codes; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.offline_order_codes (
    id bigint NOT NULL,
    order_id bigint,
    redemption_id bigint
);


--
-- Name: offline_order_codes_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.offline_order_codes_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: offline_order_codes_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.offline_order_codes_id_seq OWNED BY public.offline_order_codes.id;


--
-- Name: offline_orders; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.offline_orders (
    id bigint NOT NULL,
    order_no character varying(100),
    currency character varying(3),
    money_cents bigint,
    quota_per_code bigint,
    code_count bigint,
    paid_at bigint,
    created_at bigint,
    admin_user_id bigint,
    status character varying(16)
);


--
-- Name: offline_orders_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.offline_orders_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: offline_orders_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.offline_orders_id_seq OWNED BY public.offline_orders.id;


--
-- Name: options; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.options (
    key text NOT NULL,
    value text
);


--
-- Name: passkey_credentials; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.passkey_credentials (
    id bigint NOT NULL,
    user_id bigint NOT NULL,
    rp_id character varying(253),
    credential_id character varying(512) NOT NULL,
    public_key text NOT NULL,
    attestation_type character varying(255),
    aa_guid character varying(512),
    sign_count bigint DEFAULT 0,
    clone_warning boolean,
    user_present boolean,
    user_verified boolean,
    backup_eligible boolean,
    backup_state boolean,
    transports text,
    attachment character varying(32),
    last_used_at timestamp with time zone,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    deleted_at timestamp with time zone
);


--
-- Name: passkey_credentials_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.passkey_credentials_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: passkey_credentials_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.passkey_credentials_id_seq OWNED BY public.passkey_credentials.id;


--
-- Name: perf_metrics; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.perf_metrics (
    id bigint NOT NULL,
    model_name character varying(128),
    "group" character varying(64),
    bucket_ts bigint,
    request_count bigint DEFAULT 0,
    success_count bigint DEFAULT 0,
    total_latency_ms bigint DEFAULT 0,
    ttft_sum_ms bigint DEFAULT 0,
    ttft_count bigint DEFAULT 0,
    output_tokens bigint DEFAULT 0,
    generation_ms bigint DEFAULT 0
);


--
-- Name: perf_metrics_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.perf_metrics_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: perf_metrics_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.perf_metrics_id_seq OWNED BY public.perf_metrics.id;


--
-- Name: pool_fallback_member; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.pool_fallback_member (
    id bigint NOT NULL,
    policy_id bigint,
    account_id bigint,
    priority bigint,
    enabled boolean
);


--
-- Name: pool_fallback_member_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.pool_fallback_member_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: pool_fallback_member_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.pool_fallback_member_id_seq OWNED BY public.pool_fallback_member.id;


--
-- Name: pool_fallback_policy; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.pool_fallback_policy (
    id bigint NOT NULL,
    pool_id character varying(64),
    model_name character varying(128),
    max_fallback_count bigint,
    enabled boolean,
    created_at timestamp with time zone,
    updated_at timestamp with time zone
);


--
-- Name: pool_fallback_policy_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.pool_fallback_policy_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: pool_fallback_policy_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.pool_fallback_policy_id_seq OWNED BY public.pool_fallback_policy.id;


--
-- Name: prefill_groups; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.prefill_groups (
    id bigint NOT NULL,
    name character varying(64) NOT NULL,
    type character varying(32) NOT NULL,
    items json,
    description character varying(255),
    created_time bigint,
    updated_time bigint,
    deleted_at timestamp with time zone
);


--
-- Name: prefill_groups_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.prefill_groups_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: prefill_groups_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.prefill_groups_id_seq OWNED BY public.prefill_groups.id;


--
-- Name: quota_data; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.quota_data (
    id bigint NOT NULL,
    user_id bigint,
    username character varying(64) DEFAULT ''::character varying,
    model_name character varying(64) DEFAULT ''::character varying,
    created_at bigint,
    use_group character varying(64) DEFAULT ''::character varying,
    token_id bigint DEFAULT 0,
    channel_id bigint DEFAULT 0,
    node_name character varying(64) DEFAULT ''::character varying,
    token_used bigint DEFAULT 0,
    count bigint DEFAULT 0,
    quota bigint DEFAULT 0
);


--
-- Name: quota_data_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.quota_data_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: quota_data_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.quota_data_id_seq OWNED BY public.quota_data.id;


--
-- Name: redemption_batches; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.redemption_batches (
    id bigint NOT NULL,
    name character varying(128),
    source character varying(16) DEFAULT 'STORE'::character varying,
    type character varying(16) DEFAULT 'BALANCE'::character varying,
    face_value_cents bigint,
    quota bigint DEFAULT 0,
    count bigint DEFAULT 0,
    expired_time bigint,
    status bigint DEFAULT 1,
    note character varying(255) DEFAULT ''::character varying,
    created_by bigint,
    created_time bigint,
    updated_time bigint,
    product_id character varying(64),
    product_revision bigint,
    approval_id bigint,
    delivery_binding character varying(256),
    evidence_only boolean
);


--
-- Name: redemption_batches_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.redemption_batches_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: redemption_batches_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.redemption_batches_id_seq OWNED BY public.redemption_batches.id;


--
-- Name: redemptions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.redemptions (
    id bigint NOT NULL,
    user_id bigint,
    key character(32),
    status bigint DEFAULT 1,
    name text,
    quota bigint DEFAULT 100,
    created_time bigint,
    redeemed_time bigint,
    used_user_id bigint,
    deleted_at timestamp with time zone,
    expired_time bigint,
    batch_id bigint DEFAULT 0,
    face_value_cents bigint DEFAULT 0,
    type character varying(16) DEFAULT 'BALANCE'::character varying,
    source character varying(16) DEFAULT 'LEGACY'::character varying,
    referral_kind character varying(16)
);


--
-- Name: redemptions_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.redemptions_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: redemptions_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.redemptions_id_seq OWNED BY public.redemptions.id;


--
-- Name: referral_audits; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.referral_audits (
    id bigint NOT NULL,
    actor_id bigint,
    action character varying(32),
    details text,
    created_time bigint
);


--
-- Name: referral_audits_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.referral_audits_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: referral_audits_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.referral_audits_id_seq OWNED BY public.referral_audits.id;


--
-- Name: referral_ledgers; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.referral_ledgers (
    id bigint NOT NULL,
    reward_id bigint NOT NULL,
    user_id bigint,
    quota bigint,
    created_time bigint
);


--
-- Name: referral_ledgers_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.referral_ledgers_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: referral_ledgers_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.referral_ledgers_id_seq OWNED BY public.referral_ledgers.id;


--
-- Name: referral_rewards; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.referral_rewards (
    id bigint NOT NULL,
    redemption_id bigint NOT NULL,
    inviter_id bigint,
    invitee_id bigint,
    source_quota bigint,
    reward_quota bigint,
    rule_snapshot text,
    gate_epoch bigint,
    status character varying(24),
    created_time bigint,
    paid_time bigint,
    attempts bigint,
    last_error character varying(128)
);


--
-- Name: referral_rewards_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.referral_rewards_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: referral_rewards_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.referral_rewards_id_seq OWNED BY public.referral_rewards.id;


--
-- Name: route_chain_attempt_records; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.route_chain_attempt_records (
    id bigint NOT NULL,
    attempt_id character varying(64),
    request_id character varying(64),
    sequence bigint,
    route_chain_id bigint,
    route_chain_version bigint,
    candidate_id bigint,
    candidate_type character varying(16),
    channel_id bigint,
    channel_type bigint,
    safe_account_ref character varying(128),
    requested_model character varying(255),
    canonical_model character varying(255),
    upstream_model character varying(255),
    started_at bigint,
    dispatch_at bigint,
    finished_at bigint,
    dispatch_state character varying(16),
    terminal_state character varying(32),
    final_outcome character varying(48),
    request_written boolean,
    authoritative_rejection boolean,
    work_observed boolean,
    usage_observed boolean,
    client_output_observed boolean,
    tool_output_observed boolean,
    outcome_uncertain boolean,
    http_status bigint,
    failure_class character varying(64),
    failure_code character varying(128),
    upstream_attempt_ref character varying(128),
    upstream_response_id character varying(128),
    usage_state character varying(16),
    input_tokens bigint,
    cached_input_tokens bigint,
    output_tokens bigint,
    reasoning_tokens bigint,
    total_tokens bigint,
    provider_cost_state character varying(24),
    provider_cost_amount character varying(64),
    provider_cost_currency character varying(8),
    health_outcome character varying(24),
    breaker_effect character varying(16),
    stream_completion character varying(16),
    late_event_at bigint,
    late_event_note character varying(255),
    created_time bigint,
    updated_time bigint
);


--
-- Name: route_chain_attempt_records_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.route_chain_attempt_records_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: route_chain_attempt_records_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.route_chain_attempt_records_id_seq OWNED BY public.route_chain_attempt_records.id;


--
-- Name: route_chain_candidates; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.route_chain_candidates (
    id bigint NOT NULL,
    chain_id bigint,
    candidate_type character varying(16),
    target_id bigint,
    priority bigint,
    enabled boolean,
    archived boolean,
    archived_target_id bigint,
    policy text,
    created_time bigint,
    updated_time bigint
);


--
-- Name: route_chain_candidates_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.route_chain_candidates_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: route_chain_candidates_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.route_chain_candidates_id_seq OWNED BY public.route_chain_candidates.id;


--
-- Name: route_chain_decision_records; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.route_chain_decision_records (
    id bigint NOT NULL,
    request_id character varying(64),
    chain_id bigint,
    chain_version bigint,
    candidate_id bigint,
    phase character varying(24),
    decision character varying(16),
    reason character varying(160),
    outcome character varying(48),
    at bigint,
    created_time bigint
);


--
-- Name: route_chain_decision_records_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.route_chain_decision_records_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: route_chain_decision_records_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.route_chain_decision_records_id_seq OWNED BY public.route_chain_decision_records.id;


--
-- Name: route_chain_request_records; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.route_chain_request_records (
    id bigint NOT NULL,
    request_id character varying(64),
    route_chain_id bigint,
    route_chain_version bigint,
    requested_model character varying(255),
    canonical_model character varying(255),
    "group" character varying(64),
    transport character varying(16),
    started_at bigint,
    finished_at bigint,
    final_status character varying(32),
    final_candidate_id bigint,
    final_attempt_id character varying(64),
    final_classification character varying(48),
    user_billing_state character varying(24),
    billing_reservation_id bigint,
    created_time bigint,
    updated_time bigint
);


--
-- Name: route_chain_request_records_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.route_chain_request_records_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: route_chain_request_records_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.route_chain_request_records_id_seq OWNED BY public.route_chain_request_records.id;


--
-- Name: route_chains; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.route_chains (
    id bigint NOT NULL,
    model character varying(255),
    "group" character varying(64),
    active_scope_key character varying(64),
    enabled boolean DEFAULT false,
    allow_cross_source boolean DEFAULT false,
    archived boolean,
    version bigint DEFAULT 1,
    created_time bigint,
    updated_time bigint
);


--
-- Name: route_chains_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.route_chains_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: route_chains_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.route_chains_id_seq OWNED BY public.route_chains.id;


--
-- Name: setups; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.setups (
    id bigint NOT NULL,
    version character varying(50) NOT NULL,
    initialized_at bigint NOT NULL
);


--
-- Name: setups_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.setups_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: setups_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.setups_id_seq OWNED BY public.setups.id;


--
-- Name: store_products; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.store_products (
    id bigint NOT NULL,
    name character varying(64),
    face_value_cents bigint,
    store_url character varying(512),
    enabled boolean,
    sort bigint DEFAULT 0,
    description character varying(255) DEFAULT ''::character varying,
    created_time bigint,
    updated_time bigint
);


--
-- Name: store_products_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.store_products_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: store_products_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.store_products_id_seq OWNED BY public.store_products.id;


--
-- Name: subscription_orders; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.subscription_orders (
    payment_amount character varying(64),
    payment_currency character varying(8),
    payment_reference character varying(255),
    id bigint NOT NULL,
    user_id bigint,
    plan_id bigint,
    money numeric,
    trade_no character varying(255),
    payment_method character varying(50),
    payment_provider character varying(50) DEFAULT ''::character varying,
    status text,
    create_time bigint,
    complete_time bigint,
    provider_payload text
);


--
-- Name: subscription_orders_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.subscription_orders_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: subscription_orders_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.subscription_orders_id_seq OWNED BY public.subscription_orders.id;


--
-- Name: subscription_plans; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.subscription_plans (
    id bigint NOT NULL,
    title character varying(128) NOT NULL,
    subtitle character varying(255) DEFAULT ''::character varying,
    price_amount numeric(10,6) DEFAULT 0 NOT NULL,
    currency character varying(8) DEFAULT 'USD'::character varying NOT NULL,
    duration_unit character varying(16) DEFAULT 'month'::character varying NOT NULL,
    duration_value bigint DEFAULT 1 NOT NULL,
    custom_seconds bigint DEFAULT 0 NOT NULL,
    enabled boolean DEFAULT true,
    sort_order bigint DEFAULT 0,
    allow_balance_pay boolean,
    allow_wallet_overflow boolean,
    stripe_price_id character varying(128) DEFAULT ''::character varying,
    creem_product_id character varying(128) DEFAULT ''::character varying,
    waffo_pancake_product_id character varying(128) DEFAULT ''::character varying,
    max_purchase_per_user bigint DEFAULT 0,
    upgrade_group character varying(64) DEFAULT ''::character varying,
    downgrade_group character varying(64) DEFAULT ''::character varying,
    total_amount bigint DEFAULT 0 NOT NULL,
    quota_reset_period character varying(16) DEFAULT 'never'::character varying,
    quota_reset_custom_seconds bigint DEFAULT 0,
    created_at bigint,
    updated_at bigint
);


--
-- Name: subscription_plans_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.subscription_plans_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: subscription_plans_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.subscription_plans_id_seq OWNED BY public.subscription_plans.id;


--
-- Name: subscription_pre_consume_records; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.subscription_pre_consume_records (
    id bigint NOT NULL,
    request_id character varying(64),
    user_id bigint,
    user_subscription_id bigint,
    pre_consumed bigint DEFAULT 0 NOT NULL,
    status character varying(32),
    created_at bigint,
    updated_at bigint
);


--
-- Name: subscription_pre_consume_records_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.subscription_pre_consume_records_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: subscription_pre_consume_records_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.subscription_pre_consume_records_id_seq OWNED BY public.subscription_pre_consume_records.id;


--
-- Name: system_instances; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.system_instances (
    node_name character varying(128) NOT NULL,
    info text,
    started_at bigint,
    last_seen_at bigint,
    created_at bigint,
    updated_at bigint
);


--
-- Name: system_task_locks; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.system_task_locks (
    type character varying(64) NOT NULL,
    task_id character varying(64),
    locked_by character varying(128),
    locked_until bigint,
    updated_at bigint
);


--
-- Name: system_tasks; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.system_tasks (
    id bigint NOT NULL,
    task_id character varying(64),
    type character varying(64),
    status character varying(32),
    active_key character varying(64),
    payload text,
    state text,
    result text,
    error text,
    locked_by character varying(128),
    created_at bigint,
    updated_at bigint
);


--
-- Name: system_tasks_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.system_tasks_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: system_tasks_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.system_tasks_id_seq OWNED BY public.system_tasks.id;


--
-- Name: task_plugins; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.task_plugins (
    id bigint NOT NULL,
    key character varying(128) NOT NULL,
    api_version bigint NOT NULL,
    version character varying(64) NOT NULL,
    source text NOT NULL,
    source_hash character varying(64) NOT NULL,
    icon text,
    enabled boolean NOT NULL,
    active boolean NOT NULL,
    created_at bigint NOT NULL,
    remark text
);


--
-- Name: task_plugins_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.task_plugins_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: task_plugins_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.task_plugins_id_seq OWNED BY public.task_plugins.id;


--
-- Name: task_settlement_log_receipts; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.task_settlement_log_receipts (
    id bigint NOT NULL,
    settlement_id bigint
);


--
-- Name: task_settlement_log_receipts_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.task_settlement_log_receipts_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: task_settlement_log_receipts_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.task_settlement_log_receipts_id_seq OWNED BY public.task_settlement_log_receipts.id;


--
-- Name: task_settlements; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.task_settlements (
    id bigint NOT NULL,
    task_id bigint,
    request_id character varying(128),
    request_key character varying(128),
    user_id bigint,
    token_id bigint,
    channel_id bigint,
    subscription_id bigint,
    subscription_reset bigint,
    original_quota bigint,
    target_quota bigint,
    state character varying(24),
    error_class character varying(40),
    attempts bigint,
    next_attempt bigint,
    reason character varying(500),
    facts text,
    token_outcome character varying(24),
    log_payload text,
    log_state character varying(24),
    log_attempts bigint,
    log_next_attempt bigint,
    created_at bigint,
    updated_at bigint
);


--
-- Name: task_settlements_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.task_settlements_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: task_settlements_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.task_settlements_id_seq OWNED BY public.task_settlements.id;


--
-- Name: task_subscription_epoches; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.task_subscription_epoches (
    id bigint NOT NULL,
    request_id character varying(64),
    subscription_id bigint,
    user_id bigint,
    reset_time bigint
);


--
-- Name: task_subscription_epoches_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.task_subscription_epoches_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: task_subscription_epoches_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.task_subscription_epoches_id_seq OWNED BY public.task_subscription_epoches.id;


--
-- Name: tasks; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.tasks (
    id bigint NOT NULL,
    created_at bigint,
    updated_at bigint,
    task_id character varying(191),
    platform character varying(30),
    user_id bigint,
    "group" character varying(50),
    channel_id bigint,
    quota bigint,
    action character varying(40),
    status character varying(20),
    fail_reason text,
    submit_time bigint,
    start_time bigint,
    finish_time bigint,
    progress character varying(20),
    properties json,
    private_data json,
    data json
);


--
-- Name: tasks_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.tasks_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: tasks_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.tasks_id_seq OWNED BY public.tasks.id;


--
-- Name: tokens; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.tokens (
    id bigint NOT NULL,
    user_id bigint,
    key character varying(128),
    key_digest character varying(64),
    status bigint DEFAULT 1,
    name text,
    created_time bigint,
    accessed_time bigint,
    expired_time bigint DEFAULT '-1'::integer,
    remain_quota bigint DEFAULT 0,
    unlimited_quota boolean,
    model_limits_enabled boolean,
    model_limits text,
    allow_ips text DEFAULT ''::text,
    used_quota bigint DEFAULT 0,
    "group" text DEFAULT ''::text,
    cross_group_retry boolean,
    auto_groups text,
    deleted_at timestamp with time zone
);


--
-- Name: tokens_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.tokens_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: tokens_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.tokens_id_seq OWNED BY public.tokens.id;


--
-- Name: top_ups; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.top_ups (
    payment_amount character varying(64),
    payment_currency character varying(8),
    payment_reference character varying(255),
    id bigint NOT NULL,
    user_id bigint,
    amount bigint,
    money numeric,
    trade_no character varying(255),
    payment_method character varying(50),
    payment_provider character varying(50) DEFAULT ''::character varying,
    create_time bigint,
    complete_time bigint,
    status text
);


--
-- Name: top_ups_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.top_ups_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: top_ups_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.top_ups_id_seq OWNED BY public.top_ups.id;


--
-- Name: two_fa_backup_codes; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.two_fa_backup_codes (
    id bigint NOT NULL,
    user_id bigint NOT NULL,
    code_hash character varying(255) NOT NULL,
    is_used boolean,
    used_at timestamp with time zone,
    created_at timestamp with time zone,
    deleted_at timestamp with time zone
);


--
-- Name: two_fa_backup_codes_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.two_fa_backup_codes_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: two_fa_backup_codes_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.two_fa_backup_codes_id_seq OWNED BY public.two_fa_backup_codes.id;


--
-- Name: two_fas; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.two_fas (
    id bigint NOT NULL,
    user_id bigint NOT NULL,
    secret character varying(255) NOT NULL,
    is_enabled boolean,
    failed_attempts bigint DEFAULT 0,
    locked_until timestamp with time zone,
    last_used_at timestamp with time zone,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    deleted_at timestamp with time zone
);


--
-- Name: two_fas_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.two_fas_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: two_fas_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.two_fas_id_seq OWNED BY public.two_fas.id;


--
-- Name: user_oauth_bindings; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.user_oauth_bindings (
    id bigint NOT NULL,
    user_id bigint NOT NULL,
    provider_id bigint NOT NULL,
    provider_user_id character varying(256) NOT NULL,
    created_at timestamp with time zone
);


--
-- Name: user_oauth_bindings_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.user_oauth_bindings_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: user_oauth_bindings_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.user_oauth_bindings_id_seq OWNED BY public.user_oauth_bindings.id;


--
-- Name: user_sessions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.user_sessions (
    sid character varying(64) NOT NULL,
    user_id bigint NOT NULL,
    version bigint DEFAULT 1 NOT NULL,
    user_auth_version bigint NOT NULL,
    status character varying(16) NOT NULL,
    refresh_hash character(64) NOT NULL,
    previous_refresh_hash character varying(64),
    previous_valid_until bigint DEFAULT 0 NOT NULL,
    login_method character varying(32) NOT NULL,
    ip character varying(64),
    user_agent text,
    created_at bigint,
    last_active_at bigint NOT NULL,
    expires_at bigint NOT NULL,
    revoked_at bigint DEFAULT 0 NOT NULL,
    revoked_reason character varying(64)
);


--
-- Name: user_subscriptions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.user_subscriptions (
    id bigint NOT NULL,
    user_id bigint,
    plan_id bigint,
    amount_total bigint DEFAULT 0 NOT NULL,
    amount_used bigint DEFAULT 0 NOT NULL,
    start_time bigint,
    end_time bigint,
    status character varying(32),
    source character varying(32) DEFAULT 'order'::character varying,
    last_reset_time bigint DEFAULT 0,
    next_reset_time bigint DEFAULT 0,
    upgrade_group character varying(64) DEFAULT ''::character varying,
    prev_user_group character varying(64) DEFAULT ''::character varying,
    downgrade_group character varying(64) DEFAULT ''::character varying,
    allow_wallet_overflow boolean,
    created_at bigint,
    updated_at bigint
);


--
-- Name: user_subscriptions_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.user_subscriptions_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: user_subscriptions_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.user_subscriptions_id_seq OWNED BY public.user_subscriptions.id;


--
-- Name: users; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.users (
    id bigint NOT NULL,
    username text,
    password text NOT NULL,
    display_name text,
    role bigint DEFAULT 1,
    status bigint DEFAULT 1,
    email text,
    github_id text,
    discord_id text,
    oidc_id text,
    wechat_id text,
    telegram_id text,
    access_token character(32),
    access_token_created_at bigint,
    quota bigint DEFAULT 0,
    total_recharge_cents bigint DEFAULT 0,
    used_quota bigint DEFAULT 0,
    request_count bigint DEFAULT 0,
    "group" character varying(64) DEFAULT 'default'::character varying,
    aff_code character varying(32),
    aff_count bigint DEFAULT 0,
    aff_quota bigint DEFAULT 0,
    aff_history bigint DEFAULT 0,
    inviter_id bigint,
    deleted_at timestamp with time zone,
    linux_do_id text,
    setting text,
    remark character varying(255),
    stripe_customer character varying(64),
    created_at bigint,
    last_login_at bigint DEFAULT 0,
    auth_version bigint DEFAULT 1 NOT NULL,
    referral_paid_quota bigint DEFAULT 0 NOT NULL
);


--
-- Name: users_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.users_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: users_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.users_id_seq OWNED BY public.users.id;


--
-- Name: vendors; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.vendors (
    id bigint NOT NULL,
    name character varying(128) NOT NULL,
    description text,
    icon character varying(128),
    status bigint DEFAULT 1,
    created_time bigint,
    updated_time bigint,
    deleted_at timestamp with time zone
);


--
-- Name: vendors_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.vendors_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: vendors_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.vendors_id_seq OWNED BY public.vendors.id;


--
-- Name: vip_levels; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.vip_levels (
    id bigint NOT NULL,
    name character varying(32),
    threshold_cents bigint DEFAULT 0,
    bonus_basis_points bigint DEFAULT 0,
    enabled boolean,
    sort bigint DEFAULT 0,
    created_time bigint,
    updated_time bigint
);


--
-- Name: vip_levels_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.vip_levels_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: vip_levels_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.vip_levels_id_seq OWNED BY public.vip_levels.id;


--
-- Name: audit_logs id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.audit_logs ALTER COLUMN id SET DEFAULT nextval('public.audit_logs_id_seq'::regclass);


--
-- Name: auth_flows id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.auth_flows ALTER COLUMN id SET DEFAULT nextval('public.auth_flows_id_seq'::regclass);


--
-- Name: authz_roles id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.authz_roles ALTER COLUMN id SET DEFAULT nextval('public.authz_roles_id_seq'::regclass);


--
-- Name: balance_transactions id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.balance_transactions ALTER COLUMN id SET DEFAULT nextval('public.balance_transactions_id_seq'::regclass);


--
-- Name: billing_auth_snapshots id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.billing_auth_snapshots ALTER COLUMN id SET DEFAULT nextval('public.billing_auth_snapshots_id_seq'::regclass);


--
-- Name: billing_completion_outboxes id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.billing_completion_outboxes ALTER COLUMN id SET DEFAULT nextval('public.billing_completion_outboxes_id_seq'::regclass);


--
-- Name: billing_completion_receipts id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.billing_completion_receipts ALTER COLUMN id SET DEFAULT nextval('public.billing_completion_receipts_id_seq'::regclass);


--
-- Name: billing_reservation_decisions id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.billing_reservation_decisions ALTER COLUMN id SET DEFAULT nextval('public.billing_reservation_decisions_id_seq'::regclass);


--
-- Name: billing_reservations id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.billing_reservations ALTER COLUMN id SET DEFAULT nextval('public.billing_reservations_id_seq'::regclass);


--
-- Name: billing_review_markers id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.billing_review_markers ALTER COLUMN id SET DEFAULT nextval('public.billing_review_markers_id_seq'::regclass);


--
-- Name: casbin_rule id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.casbin_rule ALTER COLUMN id SET DEFAULT nextval('public.casbin_rule_id_seq'::regclass);


--
-- Name: channels id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.channels ALTER COLUMN id SET DEFAULT nextval('public.channels_id_seq'::regclass);


--
-- Name: checkins id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.checkins ALTER COLUMN id SET DEFAULT nextval('public.checkins_id_seq'::regclass);


--
-- Name: console_monitor_attempts id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.console_monitor_attempts ALTER COLUMN id SET DEFAULT nextval('public.console_monitor_attempts_id_seq'::regclass);


--
-- Name: console_monitors id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.console_monitors ALTER COLUMN id SET DEFAULT nextval('public.console_monitors_id_seq'::regclass);


--
-- Name: console_probes id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.console_probes ALTER COLUMN id SET DEFAULT nextval('public.console_probes_id_seq'::regclass);


--
-- Name: credit_approval_events id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.credit_approval_events ALTER COLUMN id SET DEFAULT nextval('public.credit_approval_events_id_seq'::regclass);


--
-- Name: custom_oauth_providers id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.custom_oauth_providers ALTER COLUMN id SET DEFAULT nextval('public.custom_oauth_providers_id_seq'::regclass);


--
-- Name: external_identity_claims id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.external_identity_claims ALTER COLUMN id SET DEFAULT nextval('public.external_identity_claims_id_seq'::regclass);


--
-- Name: finance_audits id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.finance_audits ALTER COLUMN id SET DEFAULT nextval('public.finance_audits_id_seq'::regclass);


--
-- Name: finance_cash_receipts id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.finance_cash_receipts ALTER COLUMN id SET DEFAULT nextval('public.finance_cash_receipts_id_seq'::regclass);


--
-- Name: finance_cash_refunds id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.finance_cash_refunds ALTER COLUMN id SET DEFAULT nextval('public.finance_cash_refunds_id_seq'::regclass);


--
-- Name: finance_expenses id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.finance_expenses ALTER COLUMN id SET DEFAULT nextval('public.finance_expenses_id_seq'::regclass);


--
-- Name: finance_rebuild_snapshots id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.finance_rebuild_snapshots ALTER COLUMN id SET DEFAULT nextval('public.finance_rebuild_snapshots_id_seq'::regclass);


--
-- Name: finance_request_events id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.finance_request_events ALTER COLUMN id SET DEFAULT nextval('public.finance_request_events_id_seq'::regclass);


--
-- Name: finance_revenue_allocations id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.finance_revenue_allocations ALTER COLUMN id SET DEFAULT nextval('public.finance_revenue_allocations_id_seq'::regclass);


--
-- Name: finance_revenue_lots id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.finance_revenue_lots ALTER COLUMN id SET DEFAULT nextval('public.finance_revenue_lots_id_seq'::regclass);


--
-- Name: finance_revenue_records id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.finance_revenue_records ALTER COLUMN id SET DEFAULT nextval('public.finance_revenue_records_id_seq'::regclass);


--
-- Name: finance_supplier_bill_lines id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.finance_supplier_bill_lines ALTER COLUMN id SET DEFAULT nextval('public.finance_supplier_bill_lines_id_seq'::regclass);


--
-- Name: finance_supplier_bills id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.finance_supplier_bills ALTER COLUMN id SET DEFAULT nextval('public.finance_supplier_bills_id_seq'::regclass);


--
-- Name: finance_sync_events id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.finance_sync_events ALTER COLUMN id SET DEFAULT nextval('public.finance_sync_events_id_seq'::regclass);


--
-- Name: finance_versions id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.finance_versions ALTER COLUMN id SET DEFAULT nextval('public.finance_versions_id_seq'::regclass);


--
-- Name: login_encryption_keys id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.login_encryption_keys ALTER COLUMN id SET DEFAULT nextval('public.login_encryption_keys_id_seq'::regclass);


--
-- Name: logs id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.logs ALTER COLUMN id SET DEFAULT nextval('public.logs_id_seq'::regclass);


--
-- Name: midjourneys id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.midjourneys ALTER COLUMN id SET DEFAULT nextval('public.midjourneys_id_seq'::regclass);


--
-- Name: models id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.models ALTER COLUMN id SET DEFAULT nextval('public.models_id_seq'::regclass);


--
-- Name: offline_order_codes id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.offline_order_codes ALTER COLUMN id SET DEFAULT nextval('public.offline_order_codes_id_seq'::regclass);


--
-- Name: offline_orders id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.offline_orders ALTER COLUMN id SET DEFAULT nextval('public.offline_orders_id_seq'::regclass);


--
-- Name: passkey_credentials id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.passkey_credentials ALTER COLUMN id SET DEFAULT nextval('public.passkey_credentials_id_seq'::regclass);


--
-- Name: perf_metrics id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.perf_metrics ALTER COLUMN id SET DEFAULT nextval('public.perf_metrics_id_seq'::regclass);


--
-- Name: pool_fallback_member id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.pool_fallback_member ALTER COLUMN id SET DEFAULT nextval('public.pool_fallback_member_id_seq'::regclass);


--
-- Name: pool_fallback_policy id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.pool_fallback_policy ALTER COLUMN id SET DEFAULT nextval('public.pool_fallback_policy_id_seq'::regclass);


--
-- Name: prefill_groups id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.prefill_groups ALTER COLUMN id SET DEFAULT nextval('public.prefill_groups_id_seq'::regclass);


--
-- Name: quota_data id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.quota_data ALTER COLUMN id SET DEFAULT nextval('public.quota_data_id_seq'::regclass);


--
-- Name: redemption_batches id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.redemption_batches ALTER COLUMN id SET DEFAULT nextval('public.redemption_batches_id_seq'::regclass);


--
-- Name: redemptions id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.redemptions ALTER COLUMN id SET DEFAULT nextval('public.redemptions_id_seq'::regclass);


--
-- Name: referral_audits id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.referral_audits ALTER COLUMN id SET DEFAULT nextval('public.referral_audits_id_seq'::regclass);


--
-- Name: referral_ledgers id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.referral_ledgers ALTER COLUMN id SET DEFAULT nextval('public.referral_ledgers_id_seq'::regclass);


--
-- Name: referral_rewards id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.referral_rewards ALTER COLUMN id SET DEFAULT nextval('public.referral_rewards_id_seq'::regclass);


--
-- Name: route_chain_attempt_records id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.route_chain_attempt_records ALTER COLUMN id SET DEFAULT nextval('public.route_chain_attempt_records_id_seq'::regclass);


--
-- Name: route_chain_candidates id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.route_chain_candidates ALTER COLUMN id SET DEFAULT nextval('public.route_chain_candidates_id_seq'::regclass);


--
-- Name: route_chain_decision_records id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.route_chain_decision_records ALTER COLUMN id SET DEFAULT nextval('public.route_chain_decision_records_id_seq'::regclass);


--
-- Name: route_chain_request_records id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.route_chain_request_records ALTER COLUMN id SET DEFAULT nextval('public.route_chain_request_records_id_seq'::regclass);


--
-- Name: route_chains id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.route_chains ALTER COLUMN id SET DEFAULT nextval('public.route_chains_id_seq'::regclass);


--
-- Name: setups id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.setups ALTER COLUMN id SET DEFAULT nextval('public.setups_id_seq'::regclass);


--
-- Name: store_products id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.store_products ALTER COLUMN id SET DEFAULT nextval('public.store_products_id_seq'::regclass);


--
-- Name: subscription_orders id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.subscription_orders ALTER COLUMN id SET DEFAULT nextval('public.subscription_orders_id_seq'::regclass);


--
-- Name: subscription_plans id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.subscription_plans ALTER COLUMN id SET DEFAULT nextval('public.subscription_plans_id_seq'::regclass);


--
-- Name: subscription_pre_consume_records id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.subscription_pre_consume_records ALTER COLUMN id SET DEFAULT nextval('public.subscription_pre_consume_records_id_seq'::regclass);


--
-- Name: system_tasks id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.system_tasks ALTER COLUMN id SET DEFAULT nextval('public.system_tasks_id_seq'::regclass);


--
-- Name: task_plugins id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.task_plugins ALTER COLUMN id SET DEFAULT nextval('public.task_plugins_id_seq'::regclass);


--
-- Name: task_settlement_log_receipts id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.task_settlement_log_receipts ALTER COLUMN id SET DEFAULT nextval('public.task_settlement_log_receipts_id_seq'::regclass);


--
-- Name: task_settlements id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.task_settlements ALTER COLUMN id SET DEFAULT nextval('public.task_settlements_id_seq'::regclass);


--
-- Name: task_subscription_epoches id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.task_subscription_epoches ALTER COLUMN id SET DEFAULT nextval('public.task_subscription_epoches_id_seq'::regclass);


--
-- Name: tasks id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.tasks ALTER COLUMN id SET DEFAULT nextval('public.tasks_id_seq'::regclass);


--
-- Name: tokens id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.tokens ALTER COLUMN id SET DEFAULT nextval('public.tokens_id_seq'::regclass);


--
-- Name: top_ups id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.top_ups ALTER COLUMN id SET DEFAULT nextval('public.top_ups_id_seq'::regclass);


--
-- Name: two_fa_backup_codes id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.two_fa_backup_codes ALTER COLUMN id SET DEFAULT nextval('public.two_fa_backup_codes_id_seq'::regclass);


--
-- Name: two_fas id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.two_fas ALTER COLUMN id SET DEFAULT nextval('public.two_fas_id_seq'::regclass);


--
-- Name: user_oauth_bindings id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_oauth_bindings ALTER COLUMN id SET DEFAULT nextval('public.user_oauth_bindings_id_seq'::regclass);


--
-- Name: user_subscriptions id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_subscriptions ALTER COLUMN id SET DEFAULT nextval('public.user_subscriptions_id_seq'::regclass);


--
-- Name: users id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.users ALTER COLUMN id SET DEFAULT nextval('public.users_id_seq'::regclass);


--
-- Name: vendors id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.vendors ALTER COLUMN id SET DEFAULT nextval('public.vendors_id_seq'::regclass);


--
-- Name: vip_levels id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.vip_levels ALTER COLUMN id SET DEFAULT nextval('public.vip_levels_id_seq'::regclass);


--
-- Name: abilities abilities_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.abilities
    ADD CONSTRAINT abilities_pkey PRIMARY KEY ("group", model, channel_id);


--
-- Name: audit_logs audit_logs_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.audit_logs
    ADD CONSTRAINT audit_logs_pkey PRIMARY KEY (id);


--
-- Name: auth_flows auth_flows_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.auth_flows
    ADD CONSTRAINT auth_flows_pkey PRIMARY KEY (id);


--
-- Name: authz_roles authz_roles_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.authz_roles
    ADD CONSTRAINT authz_roles_pkey PRIMARY KEY (id);


--
-- Name: balance_transactions balance_transactions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.balance_transactions
    ADD CONSTRAINT balance_transactions_pkey PRIMARY KEY (id);


--
-- Name: billing_auth_snapshots billing_auth_snapshots_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.billing_auth_snapshots
    ADD CONSTRAINT billing_auth_snapshots_pkey PRIMARY KEY (id);


--
-- Name: billing_completion_outboxes billing_completion_outboxes_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.billing_completion_outboxes
    ADD CONSTRAINT billing_completion_outboxes_pkey PRIMARY KEY (id);


--
-- Name: billing_completion_receipts billing_completion_receipts_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.billing_completion_receipts
    ADD CONSTRAINT billing_completion_receipts_pkey PRIMARY KEY (id);


--
-- Name: billing_reservation_decisions billing_reservation_decisions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.billing_reservation_decisions
    ADD CONSTRAINT billing_reservation_decisions_pkey PRIMARY KEY (id);


--
-- Name: billing_reservations billing_reservations_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.billing_reservations
    ADD CONSTRAINT billing_reservations_pkey PRIMARY KEY (id);


--
-- Name: billing_review_markers billing_review_markers_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.billing_review_markers
    ADD CONSTRAINT billing_review_markers_pkey PRIMARY KEY (id);


--
-- Name: casbin_rule casbin_rule_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.casbin_rule
    ADD CONSTRAINT casbin_rule_pkey PRIMARY KEY (id);


--
-- Name: channels channels_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.channels
    ADD CONSTRAINT channels_pkey PRIMARY KEY (id);


--
-- Name: checkins checkins_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.checkins
    ADD CONSTRAINT checkins_pkey PRIMARY KEY (id);


--
-- Name: console_monitor_attempts console_monitor_attempts_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.console_monitor_attempts
    ADD CONSTRAINT console_monitor_attempts_pkey PRIMARY KEY (id);


--
-- Name: console_monitor_policies console_monitor_policies_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.console_monitor_policies
    ADD CONSTRAINT console_monitor_policies_pkey PRIMARY KEY (id);


--
-- Name: console_monitors console_monitors_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.console_monitors
    ADD CONSTRAINT console_monitors_pkey PRIMARY KEY (id);


--
-- Name: console_probes console_probes_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.console_probes
    ADD CONSTRAINT console_probes_pkey PRIMARY KEY (id);


--
-- Name: credit_approval_events credit_approval_events_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.credit_approval_events
    ADD CONSTRAINT credit_approval_events_pkey PRIMARY KEY (id);


--
-- Name: custom_oauth_providers custom_oauth_providers_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.custom_oauth_providers
    ADD CONSTRAINT custom_oauth_providers_pkey PRIMARY KEY (id);


--
-- Name: external_identity_claims external_identity_claims_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.external_identity_claims
    ADD CONSTRAINT external_identity_claims_pkey PRIMARY KEY (id);


--
-- Name: finance_audits finance_audits_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.finance_audits
    ADD CONSTRAINT finance_audits_pkey PRIMARY KEY (id);


--
-- Name: finance_cash_receipts finance_cash_receipts_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.finance_cash_receipts
    ADD CONSTRAINT finance_cash_receipts_pkey PRIMARY KEY (id);


--
-- Name: finance_cash_refunds finance_cash_refunds_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.finance_cash_refunds
    ADD CONSTRAINT finance_cash_refunds_pkey PRIMARY KEY (id);


--
-- Name: finance_expenses finance_expenses_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.finance_expenses
    ADD CONSTRAINT finance_expenses_pkey PRIMARY KEY (id);


--
-- Name: finance_rebuild_snapshots finance_rebuild_snapshots_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.finance_rebuild_snapshots
    ADD CONSTRAINT finance_rebuild_snapshots_pkey PRIMARY KEY (id);


--
-- Name: finance_request_events finance_request_events_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.finance_request_events
    ADD CONSTRAINT finance_request_events_pkey PRIMARY KEY (id);


--
-- Name: finance_revenue_allocations finance_revenue_allocations_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.finance_revenue_allocations
    ADD CONSTRAINT finance_revenue_allocations_pkey PRIMARY KEY (id);


--
-- Name: finance_revenue_lots finance_revenue_lots_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.finance_revenue_lots
    ADD CONSTRAINT finance_revenue_lots_pkey PRIMARY KEY (id);


--
-- Name: finance_revenue_records finance_revenue_records_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.finance_revenue_records
    ADD CONSTRAINT finance_revenue_records_pkey PRIMARY KEY (id);


--
-- Name: finance_schemas finance_schemas_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.finance_schemas
    ADD CONSTRAINT finance_schemas_pkey PRIMARY KEY (id);


--
-- Name: finance_supplier_bill_lines finance_supplier_bill_lines_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.finance_supplier_bill_lines
    ADD CONSTRAINT finance_supplier_bill_lines_pkey PRIMARY KEY (id);


--
-- Name: finance_supplier_bills finance_supplier_bills_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.finance_supplier_bills
    ADD CONSTRAINT finance_supplier_bills_pkey PRIMARY KEY (id);


--
-- Name: finance_sync_events finance_sync_events_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.finance_sync_events
    ADD CONSTRAINT finance_sync_events_pkey PRIMARY KEY (id);


--
-- Name: finance_versions finance_versions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.finance_versions
    ADD CONSTRAINT finance_versions_pkey PRIMARY KEY (id);


--
-- Name: finance_wallets finance_wallets_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.finance_wallets
    ADD CONSTRAINT finance_wallets_pkey PRIMARY KEY (user_id);


--
-- Name: login_encryption_keys login_encryption_keys_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.login_encryption_keys
    ADD CONSTRAINT login_encryption_keys_pkey PRIMARY KEY (id);


--
-- Name: logs logs_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.logs
    ADD CONSTRAINT logs_pkey PRIMARY KEY (id);


--
-- Name: midjourneys midjourneys_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.midjourneys
    ADD CONSTRAINT midjourneys_pkey PRIMARY KEY (id);


--
-- Name: models models_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.models
    ADD CONSTRAINT models_pkey PRIMARY KEY (id);


--
-- Name: offline_order_codes offline_order_codes_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.offline_order_codes
    ADD CONSTRAINT offline_order_codes_pkey PRIMARY KEY (id);


--
-- Name: offline_orders offline_orders_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.offline_orders
    ADD CONSTRAINT offline_orders_pkey PRIMARY KEY (id);


--
-- Name: options options_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.options
    ADD CONSTRAINT options_pkey PRIMARY KEY (key);


--
-- Name: passkey_credentials passkey_credentials_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.passkey_credentials
    ADD CONSTRAINT passkey_credentials_pkey PRIMARY KEY (id);


--
-- Name: perf_metrics perf_metrics_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.perf_metrics
    ADD CONSTRAINT perf_metrics_pkey PRIMARY KEY (id);


--
-- Name: pool_fallback_member pool_fallback_member_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.pool_fallback_member
    ADD CONSTRAINT pool_fallback_member_pkey PRIMARY KEY (id);


--
-- Name: pool_fallback_policy pool_fallback_policy_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.pool_fallback_policy
    ADD CONSTRAINT pool_fallback_policy_pkey PRIMARY KEY (id);


--
-- Name: prefill_groups prefill_groups_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.prefill_groups
    ADD CONSTRAINT prefill_groups_pkey PRIMARY KEY (id);


--
-- Name: quota_data quota_data_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.quota_data
    ADD CONSTRAINT quota_data_pkey PRIMARY KEY (id);


--
-- Name: redemption_batches redemption_batches_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.redemption_batches
    ADD CONSTRAINT redemption_batches_pkey PRIMARY KEY (id);


--
-- Name: redemptions redemptions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.redemptions
    ADD CONSTRAINT redemptions_pkey PRIMARY KEY (id);


--
-- Name: referral_audits referral_audits_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.referral_audits
    ADD CONSTRAINT referral_audits_pkey PRIMARY KEY (id);


--
-- Name: referral_ledgers referral_ledgers_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.referral_ledgers
    ADD CONSTRAINT referral_ledgers_pkey PRIMARY KEY (id);


--
-- Name: referral_rewards referral_rewards_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.referral_rewards
    ADD CONSTRAINT referral_rewards_pkey PRIMARY KEY (id);


--
-- Name: route_chain_attempt_records route_chain_attempt_records_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.route_chain_attempt_records
    ADD CONSTRAINT route_chain_attempt_records_pkey PRIMARY KEY (id);


--
-- Name: route_chain_candidates route_chain_candidates_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.route_chain_candidates
    ADD CONSTRAINT route_chain_candidates_pkey PRIMARY KEY (id);


--
-- Name: route_chain_decision_records route_chain_decision_records_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.route_chain_decision_records
    ADD CONSTRAINT route_chain_decision_records_pkey PRIMARY KEY (id);


--
-- Name: route_chain_request_records route_chain_request_records_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.route_chain_request_records
    ADD CONSTRAINT route_chain_request_records_pkey PRIMARY KEY (id);


--
-- Name: route_chains route_chains_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.route_chains
    ADD CONSTRAINT route_chains_pkey PRIMARY KEY (id);


--
-- Name: setups setups_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.setups
    ADD CONSTRAINT setups_pkey PRIMARY KEY (id);


--
-- Name: store_products store_products_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.store_products
    ADD CONSTRAINT store_products_pkey PRIMARY KEY (id);


--
-- Name: subscription_orders subscription_orders_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.subscription_orders
    ADD CONSTRAINT subscription_orders_pkey PRIMARY KEY (id);


--
-- Name: subscription_plans subscription_plans_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.subscription_plans
    ADD CONSTRAINT subscription_plans_pkey PRIMARY KEY (id);


--
-- Name: subscription_pre_consume_records subscription_pre_consume_records_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.subscription_pre_consume_records
    ADD CONSTRAINT subscription_pre_consume_records_pkey PRIMARY KEY (id);


--
-- Name: system_instances system_instances_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.system_instances
    ADD CONSTRAINT system_instances_pkey PRIMARY KEY (node_name);


--
-- Name: system_task_locks system_task_locks_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.system_task_locks
    ADD CONSTRAINT system_task_locks_pkey PRIMARY KEY (type);


--
-- Name: system_tasks system_tasks_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.system_tasks
    ADD CONSTRAINT system_tasks_pkey PRIMARY KEY (id);


--
-- Name: task_plugins task_plugins_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.task_plugins
    ADD CONSTRAINT task_plugins_pkey PRIMARY KEY (id);


--
-- Name: task_settlement_log_receipts task_settlement_log_receipts_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.task_settlement_log_receipts
    ADD CONSTRAINT task_settlement_log_receipts_pkey PRIMARY KEY (id);


--
-- Name: task_settlements task_settlements_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.task_settlements
    ADD CONSTRAINT task_settlements_pkey PRIMARY KEY (id);


--
-- Name: task_subscription_epoches task_subscription_epoches_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.task_subscription_epoches
    ADD CONSTRAINT task_subscription_epoches_pkey PRIMARY KEY (id);


--
-- Name: tasks tasks_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.tasks
    ADD CONSTRAINT tasks_pkey PRIMARY KEY (id);


--
-- Name: tokens tokens_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.tokens
    ADD CONSTRAINT tokens_pkey PRIMARY KEY (id);


--
-- Name: top_ups top_ups_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.top_ups
    ADD CONSTRAINT top_ups_pkey PRIMARY KEY (id);


--
-- Name: two_fa_backup_codes two_fa_backup_codes_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.two_fa_backup_codes
    ADD CONSTRAINT two_fa_backup_codes_pkey PRIMARY KEY (id);


--
-- Name: two_fas two_fas_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.two_fas
    ADD CONSTRAINT two_fas_pkey PRIMARY KEY (id);


--
-- Name: subscription_orders uni_subscription_orders_trade_no; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.subscription_orders
    ADD CONSTRAINT uni_subscription_orders_trade_no UNIQUE (trade_no);


--
-- Name: top_ups uni_top_ups_trade_no; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.top_ups
    ADD CONSTRAINT uni_top_ups_trade_no UNIQUE (trade_no);


--
-- Name: two_fas uni_two_fas_user_id; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.two_fas
    ADD CONSTRAINT uni_two_fas_user_id UNIQUE (user_id);


--
-- Name: users uni_users_username; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.users
    ADD CONSTRAINT uni_users_username UNIQUE (username);


--
-- Name: user_oauth_bindings user_oauth_bindings_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_oauth_bindings
    ADD CONSTRAINT user_oauth_bindings_pkey PRIMARY KEY (id);


--
-- Name: user_sessions user_sessions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_sessions
    ADD CONSTRAINT user_sessions_pkey PRIMARY KEY (sid);


--
-- Name: user_subscriptions user_subscriptions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_subscriptions
    ADD CONSTRAINT user_subscriptions_pkey PRIMARY KEY (id);


--
-- Name: users users_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.users
    ADD CONSTRAINT users_pkey PRIMARY KEY (id);


--
-- Name: vendors vendors_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.vendors
    ADD CONSTRAINT vendors_pkey PRIMARY KEY (id);


--
-- Name: vip_levels vip_levels_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.vip_levels
    ADD CONSTRAINT vip_levels_pkey PRIMARY KEY (id);


--
-- Name: idx_abilities_channel_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_abilities_channel_id ON public.abilities USING btree (channel_id);


--
-- Name: idx_abilities_priority; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_abilities_priority ON public.abilities USING btree (priority);


--
-- Name: idx_abilities_tag; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_abilities_tag ON public.abilities USING btree (tag);


--
-- Name: idx_abilities_weight; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_abilities_weight ON public.abilities USING btree (weight);


--
-- Name: idx_audit_logs_category; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_audit_logs_category ON public.audit_logs USING btree (category);


--
-- Name: idx_audit_logs_created_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_audit_logs_created_at ON public.audit_logs USING btree (created_at);


--
-- Name: idx_audit_logs_event_id; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_audit_logs_event_id ON public.audit_logs USING btree (event_id);


--
-- Name: idx_audit_logs_request_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_audit_logs_request_id ON public.audit_logs USING btree (request_id);


--
-- Name: idx_audit_logs_username; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_audit_logs_username ON public.audit_logs USING btree (username);


--
-- Name: idx_audit_token_time; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_audit_token_time ON public.audit_logs USING btree (token_ref, created_at);


--
-- Name: idx_audit_user_time; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_audit_user_time ON public.audit_logs USING btree (user_id, created_at);


--
-- Name: idx_auth_flow_purpose_expiry; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_auth_flow_purpose_expiry ON public.auth_flows USING btree (purpose, expires_at);


--
-- Name: idx_auth_flows_consumed_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_auth_flows_consumed_at ON public.auth_flows USING btree (consumed_at);


--
-- Name: idx_auth_flows_session_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_auth_flows_session_id ON public.auth_flows USING btree (session_id);


--
-- Name: idx_auth_flows_token_hash; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_auth_flows_token_hash ON public.auth_flows USING btree (token_hash);


--
-- Name: idx_auth_flows_user_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_auth_flows_user_id ON public.auth_flows USING btree (user_id);


--
-- Name: idx_authz_roles_key; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_authz_roles_key ON public.authz_roles USING btree (key);


--
-- Name: idx_balance_transactions_type; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_balance_transactions_type ON public.balance_transactions USING btree (type);


--
-- Name: idx_balance_tx_user_time; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_balance_tx_user_time ON public.balance_transactions USING btree (user_id, created_at);


--
-- Name: idx_billing_auth_request_id; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_billing_auth_request_id ON public.billing_auth_snapshots USING btree (request_id);


--
-- Name: idx_billing_completion_outbox; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_billing_completion_outbox ON public.billing_completion_outboxes USING btree (request_id);


--
-- Name: idx_billing_completion_outboxes_next_attempt; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_billing_completion_outboxes_next_attempt ON public.billing_completion_outboxes USING btree (next_attempt);


--
-- Name: idx_billing_completion_outboxes_state; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_billing_completion_outboxes_state ON public.billing_completion_outboxes USING btree (state);


--
-- Name: idx_billing_completion_receipt; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_billing_completion_receipt ON public.billing_completion_receipts USING btree (request_id);


--
-- Name: idx_billing_decision_reservation; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_billing_decision_reservation ON public.billing_reservation_decisions USING btree (reservation_id);


--
-- Name: idx_billing_request_id; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_billing_request_id ON public.billing_reservations USING btree (request_id);


--
-- Name: idx_billing_reservations_state; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_billing_reservations_state ON public.billing_reservations USING btree (state);


--
-- Name: idx_billing_review_reservation; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_billing_review_reservation ON public.billing_review_markers USING btree (reservation_id);


--
-- Name: idx_casbin_rule; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_casbin_rule ON public.casbin_rule USING btree (ptype, v0, v1, v2, v3, v4, v5);


--
-- Name: idx_casbin_rule_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_casbin_rule_unique ON public.casbin_rule USING btree (ptype, v0, v1, v2, v3, v4, v5);


--
-- Name: idx_channels_name; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_channels_name ON public.channels USING btree (name);


--
-- Name: idx_channels_tag; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_channels_tag ON public.channels USING btree (tag);


--
-- Name: idx_console_monitor_attempts_cleanup_state; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_console_monitor_attempts_cleanup_state ON public.console_monitor_attempts USING btree (cleanup_state);


--
-- Name: idx_console_monitor_attempts_lease_until; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_console_monitor_attempts_lease_until ON public.console_monitor_attempts USING btree (lease_until);


--
-- Name: idx_console_monitor_attempts_monitor_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_console_monitor_attempts_monitor_id ON public.console_monitor_attempts USING btree (monitor_id);


--
-- Name: idx_console_monitor_attempts_started_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_console_monitor_attempts_started_at ON public.console_monitor_attempts USING btree (started_at);


--
-- Name: idx_console_monitors_channel_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_console_monitors_channel_id ON public.console_monitors USING btree (channel_id);


--
-- Name: idx_console_monitors_identity; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_console_monitors_identity ON public.console_monitors USING btree (identity);


--
-- Name: idx_console_probe_time; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_console_probe_time ON public.console_probes USING btree (monitor_id, checked_at);


--
-- Name: idx_created_at_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_created_at_id ON public.logs USING btree (created_at, id);


--
-- Name: idx_created_at_type; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_created_at_type ON public.logs USING btree (created_at, type);


--
-- Name: idx_credit_approval_events_product_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_credit_approval_events_product_id ON public.credit_approval_events USING btree (product_id);


--
-- Name: idx_credit_approval_events_reference_approval_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_credit_approval_events_reference_approval_id ON public.credit_approval_events USING btree (reference_approval_id);


--
-- Name: idx_custom_oauth_providers_slug; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_custom_oauth_providers_slug ON public.custom_oauth_providers USING btree (slug);


--
-- Name: idx_external_identity_claims_user_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_external_identity_claims_user_id ON public.external_identity_claims USING btree (user_id);


--
-- Name: idx_external_identity_subject; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_external_identity_subject ON public.external_identity_claims USING btree (provider, subject);


--
-- Name: idx_external_identity_user; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_external_identity_user ON public.external_identity_claims USING btree (provider, user_id);


--
-- Name: idx_fin_bill_revision; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_fin_bill_revision ON public.finance_supplier_bills USING btree (channel_id, revision);


--
-- Name: idx_fin_revenue_key; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_fin_revenue_key ON public.finance_revenue_records USING btree (user_id, event_key);


--
-- Name: idx_fin_revenue_seq; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_fin_revenue_seq ON public.finance_revenue_records USING btree (user_id, revision);


--
-- Name: idx_fin_version; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_fin_version ON public.finance_versions USING btree (kind, record_key, revision);


--
-- Name: idx_finance_cash_receipts_order_id; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_finance_cash_receipts_order_id ON public.finance_cash_receipts USING btree (order_id);


--
-- Name: idx_finance_cash_receipts_paid_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_finance_cash_receipts_paid_at ON public.finance_cash_receipts USING btree (paid_at);


--
-- Name: idx_finance_cash_refunds_event_key; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_finance_cash_refunds_event_key ON public.finance_cash_refunds USING btree (event_key);


--
-- Name: idx_finance_cash_refunds_order_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_finance_cash_refunds_order_id ON public.finance_cash_refunds USING btree (order_id);


--
-- Name: idx_finance_cash_refunds_receipt_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_finance_cash_refunds_receipt_id ON public.finance_cash_refunds USING btree (receipt_id);


--
-- Name: idx_finance_cash_refunds_refunded_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_finance_cash_refunds_refunded_at ON public.finance_cash_refunds USING btree (refunded_at);


--
-- Name: idx_finance_expenses_event_key; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_finance_expenses_event_key ON public.finance_expenses USING btree (event_key);


--
-- Name: idx_finance_rebuild_snapshots_user_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_finance_rebuild_snapshots_user_id ON public.finance_rebuild_snapshots USING btree (user_id);


--
-- Name: idx_finance_request_events_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_finance_request_events_at ON public.finance_request_events USING btree (at);


--
-- Name: idx_finance_request_events_attempt_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_finance_request_events_attempt_id ON public.finance_request_events USING btree (attempt_id);


--
-- Name: idx_finance_request_events_channel_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_finance_request_events_channel_id ON public.finance_request_events USING btree (channel_id);


--
-- Name: idx_finance_request_events_event_key; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_finance_request_events_event_key ON public.finance_request_events USING btree (event_key);


--
-- Name: idx_finance_request_events_request_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_finance_request_events_request_id ON public.finance_request_events USING btree (request_id);


--
-- Name: idx_finance_revenue_allocations_event_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_finance_revenue_allocations_event_id ON public.finance_revenue_allocations USING btree (event_id);


--
-- Name: idx_finance_revenue_allocations_lot_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_finance_revenue_allocations_lot_id ON public.finance_revenue_allocations USING btree (lot_id);


--
-- Name: idx_finance_revenue_lots_event_id; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_finance_revenue_lots_event_id ON public.finance_revenue_lots USING btree (event_id);


--
-- Name: idx_finance_revenue_lots_user_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_finance_revenue_lots_user_id ON public.finance_revenue_lots USING btree (user_id);


--
-- Name: idx_finance_revenue_records_reverses; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_finance_revenue_records_reverses ON public.finance_revenue_records USING btree (reverses);


--
-- Name: idx_finance_supplier_bill_lines_bill_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_finance_supplier_bill_lines_bill_id ON public.finance_supplier_bill_lines USING btree (bill_id);


--
-- Name: idx_finance_supplier_bill_lines_channel_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_finance_supplier_bill_lines_channel_id ON public.finance_supplier_bill_lines USING btree (channel_id);


--
-- Name: idx_finance_supplier_bill_lines_event_key; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_finance_supplier_bill_lines_event_key ON public.finance_supplier_bill_lines USING btree (event_key);


--
-- Name: idx_finance_supplier_bills_event_key; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_finance_supplier_bills_event_key ON public.finance_supplier_bills USING btree (event_key);


--
-- Name: idx_finance_sync_events_event_key; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_finance_sync_events_event_key ON public.finance_sync_events USING btree (event_key);


--
-- Name: idx_finance_sync_events_next_attempt_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_finance_sync_events_next_attempt_at ON public.finance_sync_events USING btree (next_attempt_at);


--
-- Name: idx_finance_sync_events_ref_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_finance_sync_events_ref_id ON public.finance_sync_events USING btree (ref_id);


--
-- Name: idx_finance_sync_events_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_finance_sync_events_status ON public.finance_sync_events USING btree (status);


--
-- Name: idx_finance_sync_events_topic; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_finance_sync_events_topic ON public.finance_sync_events USING btree (topic);


--
-- Name: idx_finance_sync_events_user_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_finance_sync_events_user_id ON public.finance_sync_events USING btree (user_id);


--
-- Name: idx_login_encryption_keys_slot; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_login_encryption_keys_slot ON public.login_encryption_keys USING btree (slot);


--
-- Name: idx_logs_channel_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_logs_channel_id ON public.logs USING btree (channel_id);


--
-- Name: idx_logs_group; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_logs_group ON public.logs USING btree ("group");


--
-- Name: idx_logs_ip; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_logs_ip ON public.logs USING btree (ip);


--
-- Name: idx_logs_model_name; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_logs_model_name ON public.logs USING btree (model_name);


--
-- Name: idx_logs_request_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_logs_request_id ON public.logs USING btree (request_id);


--
-- Name: idx_logs_token_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_logs_token_id ON public.logs USING btree (token_id);


--
-- Name: idx_logs_token_name; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_logs_token_name ON public.logs USING btree (token_name);


--
-- Name: idx_logs_upstream_request_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_logs_upstream_request_id ON public.logs USING btree (upstream_request_id);


--
-- Name: idx_logs_user_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_logs_user_id ON public.logs USING btree (user_id);


--
-- Name: idx_logs_username; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_logs_username ON public.logs USING btree (username);


--
-- Name: idx_midjourneys_action; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_midjourneys_action ON public.midjourneys USING btree (action);


--
-- Name: idx_midjourneys_finish_time; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_midjourneys_finish_time ON public.midjourneys USING btree (finish_time);


--
-- Name: idx_midjourneys_mj_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_midjourneys_mj_id ON public.midjourneys USING btree (mj_id);


--
-- Name: idx_midjourneys_progress; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_midjourneys_progress ON public.midjourneys USING btree (progress);


--
-- Name: idx_midjourneys_start_time; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_midjourneys_start_time ON public.midjourneys USING btree (start_time);


--
-- Name: idx_midjourneys_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_midjourneys_status ON public.midjourneys USING btree (status);


--
-- Name: idx_midjourneys_submit_time; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_midjourneys_submit_time ON public.midjourneys USING btree (submit_time);


--
-- Name: idx_midjourneys_user_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_midjourneys_user_id ON public.midjourneys USING btree (user_id);


--
-- Name: idx_models_deleted_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_models_deleted_at ON public.models USING btree (deleted_at);


--
-- Name: idx_models_vendor_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_models_vendor_id ON public.models USING btree (vendor_id);


--
-- Name: idx_offline_order_codes_order_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_offline_order_codes_order_id ON public.offline_order_codes USING btree (order_id);


--
-- Name: idx_offline_order_codes_redemption_id; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_offline_order_codes_redemption_id ON public.offline_order_codes USING btree (redemption_id);


--
-- Name: idx_offline_orders_order_no; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_offline_orders_order_no ON public.offline_orders USING btree (order_no);


--
-- Name: idx_passkey_credentials_credential_id; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_passkey_credentials_credential_id ON public.passkey_credentials USING btree (credential_id);


--
-- Name: idx_passkey_credentials_deleted_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_passkey_credentials_deleted_at ON public.passkey_credentials USING btree (deleted_at);


--
-- Name: idx_passkey_credentials_user_id; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_passkey_credentials_user_id ON public.passkey_credentials USING btree (user_id);


--
-- Name: idx_perf_bucket_ts; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_perf_bucket_ts ON public.perf_metrics USING btree (bucket_ts);


--
-- Name: idx_perf_model_group_bucket; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_perf_model_group_bucket ON public.perf_metrics USING btree (model_name, "group", bucket_ts);


--
-- Name: idx_pool_fallback_member_account; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_pool_fallback_member_account ON public.pool_fallback_member USING btree (policy_id, account_id);


--
-- Name: idx_pool_fallback_member_order; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_pool_fallback_member_order ON public.pool_fallback_member USING btree (policy_id, priority);


--
-- Name: idx_pool_fallback_scope; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_pool_fallback_scope ON public.pool_fallback_policy USING btree (pool_id, model_name);


--
-- Name: idx_prefill_groups_deleted_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_prefill_groups_deleted_at ON public.prefill_groups USING btree (deleted_at);


--
-- Name: idx_prefill_groups_type; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_prefill_groups_type ON public.prefill_groups USING btree (type);


--
-- Name: idx_qdt_created_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_qdt_created_at ON public.quota_data USING btree (created_at);


--
-- Name: idx_qdt_model_user_name; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_qdt_model_user_name ON public.quota_data USING btree (model_name, username);


--
-- Name: idx_quota_data_channel_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_quota_data_channel_id ON public.quota_data USING btree (channel_id);


--
-- Name: idx_quota_data_node_name; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_quota_data_node_name ON public.quota_data USING btree (node_name);


--
-- Name: idx_quota_data_token_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_quota_data_token_id ON public.quota_data USING btree (token_id);


--
-- Name: idx_quota_data_use_group; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_quota_data_use_group ON public.quota_data USING btree (use_group);


--
-- Name: idx_quota_data_user_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_quota_data_user_id ON public.quota_data USING btree (user_id);


--
-- Name: idx_redemption_batches_created_time; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_redemption_batches_created_time ON public.redemption_batches USING btree (created_time);


--
-- Name: idx_redemption_batches_name; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_redemption_batches_name ON public.redemption_batches USING btree (name);


--
-- Name: idx_redemption_batches_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_redemption_batches_status ON public.redemption_batches USING btree (status);


--
-- Name: idx_redemptions_batch_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_redemptions_batch_id ON public.redemptions USING btree (batch_id);


--
-- Name: idx_redemptions_deleted_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_redemptions_deleted_at ON public.redemptions USING btree (deleted_at);


--
-- Name: idx_redemptions_key; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_redemptions_key ON public.redemptions USING btree (key);


--
-- Name: idx_redemptions_name; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_redemptions_name ON public.redemptions USING btree (name);


--
-- Name: idx_redemptions_source; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_redemptions_source ON public.redemptions USING btree (source);


--
-- Name: idx_redemptions_type; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_redemptions_type ON public.redemptions USING btree (type);


--
-- Name: idx_referral_ledgers_reward_id; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_referral_ledgers_reward_id ON public.referral_ledgers USING btree (reward_id);


--
-- Name: idx_referral_ledgers_user_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_referral_ledgers_user_id ON public.referral_ledgers USING btree (user_id);


--
-- Name: idx_referral_rewards_inviter_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_referral_rewards_inviter_id ON public.referral_rewards USING btree (inviter_id);


--
-- Name: idx_referral_rewards_redemption_id; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_referral_rewards_redemption_id ON public.referral_rewards USING btree (redemption_id);


--
-- Name: idx_referral_rewards_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_referral_rewards_status ON public.referral_rewards USING btree (status);


--
-- Name: idx_route_attempt_req_seq; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_route_attempt_req_seq ON public.route_chain_attempt_records USING btree (request_id, sequence);


--
-- Name: idx_route_chain_active_scope; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_route_chain_active_scope ON public.route_chains USING btree (active_scope_key);


--
-- Name: idx_route_chain_attempt_records_attempt_id; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_route_chain_attempt_records_attempt_id ON public.route_chain_attempt_records USING btree (attempt_id);


--
-- Name: idx_route_chain_attempt_records_candidate_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_route_chain_attempt_records_candidate_id ON public.route_chain_attempt_records USING btree (candidate_id);


--
-- Name: idx_route_chain_attempt_records_channel_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_route_chain_attempt_records_channel_id ON public.route_chain_attempt_records USING btree (channel_id);


--
-- Name: idx_route_chain_attempt_records_dispatch_state; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_route_chain_attempt_records_dispatch_state ON public.route_chain_attempt_records USING btree (dispatch_state);


--
-- Name: idx_route_chain_attempt_records_provider_cost_state; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_route_chain_attempt_records_provider_cost_state ON public.route_chain_attempt_records USING btree (provider_cost_state);


--
-- Name: idx_route_chain_attempt_records_route_chain_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_route_chain_attempt_records_route_chain_id ON public.route_chain_attempt_records USING btree (route_chain_id);


--
-- Name: idx_route_chain_attempt_records_terminal_state; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_route_chain_attempt_records_terminal_state ON public.route_chain_attempt_records USING btree (terminal_state);


--
-- Name: idx_route_chain_candidate_target; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_route_chain_candidate_target ON public.route_chain_candidates USING btree (chain_id, candidate_type, target_id);


--
-- Name: idx_route_chain_candidates_chain_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_route_chain_candidates_chain_id ON public.route_chain_candidates USING btree (chain_id);


--
-- Name: idx_route_chain_candidates_priority; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_route_chain_candidates_priority ON public.route_chain_candidates USING btree (priority);


--
-- Name: idx_route_chain_decision_records_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_route_chain_decision_records_at ON public.route_chain_decision_records USING btree (at);


--
-- Name: idx_route_chain_decision_records_candidate_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_route_chain_decision_records_candidate_id ON public.route_chain_decision_records USING btree (candidate_id);


--
-- Name: idx_route_chain_decision_records_request_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_route_chain_decision_records_request_id ON public.route_chain_decision_records USING btree (request_id);


--
-- Name: idx_route_chain_request_records_final_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_route_chain_request_records_final_status ON public.route_chain_request_records USING btree (final_status);


--
-- Name: idx_route_chain_request_records_request_id; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_route_chain_request_records_request_id ON public.route_chain_request_records USING btree (request_id);


--
-- Name: idx_route_chain_request_records_route_chain_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_route_chain_request_records_route_chain_id ON public.route_chain_request_records USING btree (route_chain_id);


--
-- Name: idx_route_chain_request_records_started_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_route_chain_request_records_started_at ON public.route_chain_request_records USING btree (started_at);


--
-- Name: idx_route_chain_request_records_user_billing_state; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_route_chain_request_records_user_billing_state ON public.route_chain_request_records USING btree (user_billing_state);


--
-- Name: idx_store_products_enabled_sort; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_store_products_enabled_sort ON public.store_products USING btree (enabled, sort);


--
-- Name: idx_subscription_orders_plan_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_subscription_orders_plan_id ON public.subscription_orders USING btree (plan_id);


--
-- Name: idx_subscription_orders_trade_no; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_subscription_orders_trade_no ON public.subscription_orders USING btree (trade_no);


--
-- Name: idx_subscription_orders_user_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_subscription_orders_user_id ON public.subscription_orders USING btree (user_id);


--
-- Name: idx_subscription_pre_consume_records_request_id; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_subscription_pre_consume_records_request_id ON public.subscription_pre_consume_records USING btree (request_id);


--
-- Name: idx_subscription_pre_consume_records_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_subscription_pre_consume_records_status ON public.subscription_pre_consume_records USING btree (status);


--
-- Name: idx_subscription_pre_consume_records_updated_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_subscription_pre_consume_records_updated_at ON public.subscription_pre_consume_records USING btree (updated_at);


--
-- Name: idx_subscription_pre_consume_records_user_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_subscription_pre_consume_records_user_id ON public.subscription_pre_consume_records USING btree (user_id);


--
-- Name: idx_subscription_pre_consume_records_user_subscription_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_subscription_pre_consume_records_user_subscription_id ON public.subscription_pre_consume_records USING btree (user_subscription_id);


--
-- Name: idx_system_instances_created_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_system_instances_created_at ON public.system_instances USING btree (created_at);


--
-- Name: idx_system_instances_last_seen_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_system_instances_last_seen_at ON public.system_instances USING btree (last_seen_at);


--
-- Name: idx_system_instances_started_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_system_instances_started_at ON public.system_instances USING btree (started_at);


--
-- Name: idx_system_instances_updated_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_system_instances_updated_at ON public.system_instances USING btree (updated_at);


--
-- Name: idx_system_task_locks_locked_by; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_system_task_locks_locked_by ON public.system_task_locks USING btree (locked_by);


--
-- Name: idx_system_task_locks_locked_until; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_system_task_locks_locked_until ON public.system_task_locks USING btree (locked_until);


--
-- Name: idx_system_task_locks_task_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_system_task_locks_task_id ON public.system_task_locks USING btree (task_id);


--
-- Name: idx_system_task_locks_updated_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_system_task_locks_updated_at ON public.system_task_locks USING btree (updated_at);


--
-- Name: idx_system_tasks_active_key; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_system_tasks_active_key ON public.system_tasks USING btree (active_key);


--
-- Name: idx_system_tasks_created_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_system_tasks_created_at ON public.system_tasks USING btree (created_at);


--
-- Name: idx_system_tasks_locked_by; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_system_tasks_locked_by ON public.system_tasks USING btree (locked_by);


--
-- Name: idx_system_tasks_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_system_tasks_status ON public.system_tasks USING btree (status);


--
-- Name: idx_system_tasks_task_id; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_system_tasks_task_id ON public.system_tasks USING btree (task_id);


--
-- Name: idx_system_tasks_type; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_system_tasks_type ON public.system_tasks USING btree (type);


--
-- Name: idx_system_tasks_updated_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_system_tasks_updated_at ON public.system_tasks USING btree (updated_at);


--
-- Name: idx_task_plugins_active; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_task_plugins_active ON public.task_plugins USING btree (active);


--
-- Name: idx_task_settlement_log; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_task_settlement_log ON public.task_settlement_log_receipts USING btree (settlement_id);


--
-- Name: idx_task_settlement_request; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_task_settlement_request ON public.task_settlements USING btree (request_key);


--
-- Name: idx_task_settlement_task; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_task_settlement_task ON public.task_settlements USING btree (task_id);


--
-- Name: idx_task_settlements_request_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_task_settlements_request_id ON public.task_settlements USING btree (request_id);


--
-- Name: idx_task_settlements_state; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_task_settlements_state ON public.task_settlements USING btree (state);


--
-- Name: idx_task_subscription_epoch; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_task_subscription_epoch ON public.task_subscription_epoches USING btree (request_id);


--
-- Name: idx_tasks_action; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_tasks_action ON public.tasks USING btree (action);


--
-- Name: idx_tasks_channel_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_tasks_channel_id ON public.tasks USING btree (channel_id);


--
-- Name: idx_tasks_created_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_tasks_created_at ON public.tasks USING btree (created_at);


--
-- Name: idx_tasks_finish_time; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_tasks_finish_time ON public.tasks USING btree (finish_time);


--
-- Name: idx_tasks_platform; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_tasks_platform ON public.tasks USING btree (platform);


--
-- Name: idx_tasks_progress; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_tasks_progress ON public.tasks USING btree (progress);


--
-- Name: idx_tasks_start_time; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_tasks_start_time ON public.tasks USING btree (start_time);


--
-- Name: idx_tasks_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_tasks_status ON public.tasks USING btree (status);


--
-- Name: idx_tasks_submit_time; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_tasks_submit_time ON public.tasks USING btree (submit_time);


--
-- Name: idx_tasks_task_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_tasks_task_id ON public.tasks USING btree (task_id);


--
-- Name: idx_tasks_user_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_tasks_user_id ON public.tasks USING btree (user_id);


--
-- Name: idx_token_digest; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_token_digest ON public.tokens USING btree (key_digest);


--
-- Name: idx_tokens_deleted_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_tokens_deleted_at ON public.tokens USING btree (deleted_at);


--
-- Name: idx_tokens_key; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_tokens_key ON public.tokens USING btree (key);


--
-- Name: idx_tokens_name; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_tokens_name ON public.tokens USING btree (name);


--
-- Name: idx_tokens_user_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_tokens_user_id ON public.tokens USING btree (user_id);


--
-- Name: idx_top_ups_trade_no; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_top_ups_trade_no ON public.top_ups USING btree (trade_no);


--
-- Name: idx_top_ups_user_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_top_ups_user_id ON public.top_ups USING btree (user_id);


--
-- Name: idx_two_fa_backup_codes_deleted_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_two_fa_backup_codes_deleted_at ON public.two_fa_backup_codes USING btree (deleted_at);


--
-- Name: idx_two_fa_backup_codes_user_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_two_fa_backup_codes_user_id ON public.two_fa_backup_codes USING btree (user_id);


--
-- Name: idx_two_fas_deleted_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_two_fas_deleted_at ON public.two_fas USING btree (deleted_at);


--
-- Name: idx_two_fas_user_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_two_fas_user_id ON public.two_fas USING btree (user_id);


--
-- Name: idx_user_checkin_date; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_user_checkin_date ON public.checkins USING btree (user_id, checkin_date);


--
-- Name: idx_user_id_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_user_id_id ON public.logs USING btree (user_id, id);


--
-- Name: idx_user_sessions_expires_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_user_sessions_expires_at ON public.user_sessions USING btree (expires_at);


--
-- Name: idx_user_sessions_status_revoked; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_user_sessions_status_revoked ON public.user_sessions USING btree (status, revoked_at);


--
-- Name: idx_user_sessions_user_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_user_sessions_user_created ON public.user_sessions USING btree (user_id, created_at);


--
-- Name: idx_user_sessions_user_status_expiry; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_user_sessions_user_status_expiry ON public.user_sessions USING btree (user_id, status, expires_at);


--
-- Name: idx_user_sub_active; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_user_sub_active ON public.user_subscriptions USING btree (user_id, status, end_time);


--
-- Name: idx_user_subscriptions_end_time; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_user_subscriptions_end_time ON public.user_subscriptions USING btree (end_time);


--
-- Name: idx_user_subscriptions_next_reset_time; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_user_subscriptions_next_reset_time ON public.user_subscriptions USING btree (next_reset_time);


--
-- Name: idx_user_subscriptions_plan_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_user_subscriptions_plan_id ON public.user_subscriptions USING btree (plan_id);


--
-- Name: idx_user_subscriptions_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_user_subscriptions_status ON public.user_subscriptions USING btree (status);


--
-- Name: idx_user_subscriptions_user_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_user_subscriptions_user_id ON public.user_subscriptions USING btree (user_id);


--
-- Name: idx_users_access_token; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_users_access_token ON public.users USING btree (access_token);


--
-- Name: idx_users_aff_code; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_users_aff_code ON public.users USING btree (aff_code);


--
-- Name: idx_users_deleted_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_users_deleted_at ON public.users USING btree (deleted_at);


--
-- Name: idx_users_discord_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_users_discord_id ON public.users USING btree (discord_id);


--
-- Name: idx_users_display_name; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_users_display_name ON public.users USING btree (display_name);


--
-- Name: idx_users_email; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_users_email ON public.users USING btree (email);


--
-- Name: idx_users_git_hub_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_users_git_hub_id ON public.users USING btree (github_id);


--
-- Name: idx_users_inviter_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_users_inviter_id ON public.users USING btree (inviter_id);


--
-- Name: idx_users_linux_do_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_users_linux_do_id ON public.users USING btree (linux_do_id);


--
-- Name: idx_users_oidc_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_users_oidc_id ON public.users USING btree (oidc_id);


--
-- Name: idx_users_stripe_customer; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_users_stripe_customer ON public.users USING btree (stripe_customer);


--
-- Name: idx_users_telegram_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_users_telegram_id ON public.users USING btree (telegram_id);


--
-- Name: idx_users_username; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_users_username ON public.users USING btree (username);


--
-- Name: idx_users_we_chat_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_users_we_chat_id ON public.users USING btree (wechat_id);


--
-- Name: idx_vendors_deleted_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_vendors_deleted_at ON public.vendors USING btree (deleted_at);


--
-- Name: idx_vip_levels_threshold_cents; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_vip_levels_threshold_cents ON public.vip_levels USING btree (threshold_cents);


--
-- Name: index_username_model_name; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX index_username_model_name ON public.logs USING btree (model_name, username);


--
-- Name: uk_model_name_delete_at; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uk_model_name_delete_at ON public.models USING btree (model_name, deleted_at);


--
-- Name: uk_prefill_name; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uk_prefill_name ON public.prefill_groups USING btree (name) WHERE (deleted_at IS NULL);


--
-- Name: uk_task_plugin_key_version; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uk_task_plugin_key_version ON public.task_plugins USING btree (key, version);


--
-- Name: uk_vendor_name_delete_at; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uk_vendor_name_delete_at ON public.vendors USING btree (name, deleted_at);


--
-- Name: ux_provider_userid; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX ux_provider_userid ON public.user_oauth_bindings USING btree (provider_id, provider_user_id);


--
-- Name: ux_user_provider; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX ux_user_provider ON public.user_oauth_bindings USING btree (user_id, provider_id);


--
-- Name: offline_order_codes fk_offline_order_codes_redemption; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.offline_order_codes
    ADD CONSTRAINT fk_offline_order_codes_redemption FOREIGN KEY (redemption_id) REFERENCES public.redemptions(id);


--
-- Name: offline_order_codes fk_offline_orders_codes; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.offline_order_codes
    ADD CONSTRAINT fk_offline_orders_codes FOREIGN KEY (order_id) REFERENCES public.offline_orders(id);


--
-- Name: pool_fallback_member fk_pool_fallback_policy_members; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.pool_fallback_member
    ADD CONSTRAINT fk_pool_fallback_policy_members FOREIGN KEY (policy_id) REFERENCES public.pool_fallback_policy(id) ON DELETE CASCADE;


--
-- PostgreSQL database dump complete
--

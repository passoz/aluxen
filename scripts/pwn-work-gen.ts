#!/usr/bin/env bun
/**
 * Gerador de cadeia normativa do pwn para o port Go do Aluxen.
 *
 * Lê uma especificação declarativa (`.piwerness/specs/<work>.json`) e escreve a
 * cadeia completa e schema-válida de um Work:
 *   discovery.json, requirements.json, prd.json, spec.json, plan.json,
 *   CTR-NNN.json (contrato V4 por task), traceability-matrix.json.
 *
 * Uso: bun scripts/pwn-work-gen.ts .piwerness/specs/0001.json
 *
 * O gerador não substitui os portões: depois de gerar, rode
 * `pwn work contract`, os 5 gates e `pwn validate`.
 */
import fs from 'node:fs';
import path from 'node:path';

type Requirement = {
  id: string;
  title: string;
  user_story: string;
  acceptance_criteria: string[];
  priority?: 'must' | 'should' | 'could';
};

type Capability = {
  id: string;
  name: string;
  description: string;
  rules: string[];
};

type Rule = {
  id: string;
  statement: string;
  coverage: string[];
  evidence: string;
};

type Task = {
  id: string;
  title: string;
  requirement_id: string;
  spec_reference: string;
  behavior: string;
  components: string[];
  files: string[];
  implementation_files: string[];
  test_files: string[];
  implementation_steps: string[];
  acceptance_criteria: { command: string; description: string }[];
  depends_on?: string[];
  risk?: 'L0' | 'L1' | 'L2' | 'L3' | 'L4';
  write_allow?: string[];
  write_deny?: string[];
  out_of_scope?: string[];
  visual?: 'N/A' | 'REQUIRED';
  documentation?: 'N/A' | 'REQUIRED';
  complexity?: string;
};

type WorkSpec = {
  work_id: string;
  title: string;
  scenario: 'greenfield' | 'brownfield';
  problem: string;
  solution: string;
  actors: { id: string; name: string; description: string }[];
  constraints: string[];
  objectives: string[];
  decisions: { id: string; statement: string; status: string; made_by: string }[];
  requirements: Requirement[];
  capabilities: Capability[];
  rules: Rule[];
  components: { name: string; purpose: string }[];
  global_gates: string[];
  tasks: Task[];
  default_write_allow: string[];
  default_write_deny: string[];
  acceptance: { command: string; description: string }[];
  legacy_spec?: string;
};

const ROOT = path.resolve(import.meta.dir, '..');

function readSpec(specPath: string): WorkSpec {
  return JSON.parse(fs.readFileSync(specPath, 'utf8')) as WorkSpec;
}

function writeJson(filePath: string, value: unknown): void {
  fs.mkdirSync(path.dirname(filePath), { recursive: true });
  fs.writeFileSync(filePath, `${JSON.stringify(value, null, 2)}\n`);
}

function workDir(spec: WorkSpec): string {
  return path.join(ROOT, '.piwerness', 'work', spec.work_id);
}

function contractFor(spec: WorkSpec, task: Task, index: number): Record<string, unknown> {
  const risk = task.risk ?? 'L2';
  const requirements = spec.requirements.filter((r) => r.id === task.requirement_id);
  return {
    contract_version: '4.0',
    task_id: task.id,
    work_id: spec.work_id,
    title: task.title,
    risk: { level: risk, reasons: [`Task ${task.id} classificada como ${risk} na especificação do Work`] },
    validation_strategy: 'tdd-strict',
    behavioral_contract: {
      scenarios: [
        {
          id: `SCENARIO-${task.id}-01`,
          given: `Dado o Work ${spec.work_id} ("${spec.title}") e a task ${task.id} ("${task.title}")`,
          when: `Quando o executor implementa a task ${task.id}`,
          then: `Então ${task.behavior}`,
        },
        {
          id: `SCENARIO-${task.id}-02`,
          given: `Dado que os critérios de aceite da task ${task.id} são ${task.acceptance_criteria.map((c) => `\`${c.command}\``).join(', ')}`,
          when: 'Quando os comandos de aceite são executados no sandbox',
          then: 'Então todos saem com exit 0 e nenhum arquivo fora da write_allow é modificado',
        },
        ...requirements.flatMap((r) =>
          r.acceptance_criteria.map((criterion, i) => ({
            id: `SCENARIO-${task.id}-REQ-${i + 1}`,
            given: `Dado o requisito ${r.id} ("${r.title}")`,
            when: `Quando o comportamento descrito em "${criterion}" é exercitado`,
            then: 'Então o resultado observável declarado no critério é verificado por teste automatizado',
          })),
        ),
      ],
    },
    change_contract: {
      target_files: task.implementation_files,
      affected_components: task.components,
    },
    architecture_contract: {
      invariants: [
        'domain não importa application/infrastructure; infrastructure importa domain',
        'Nenhum handler contém regra de negócio; nenhum acesso a dados monta SQL em runtime',
        'O uso atual (contrato residencial, recibo mensal, baixa) permanece idêntico — toda mudança é aditiva',
      ],
    },
    acceptance_contract: {
      commands: [...new Set([...task.acceptance_criteria.map((c) => c.command), ...spec.acceptance.map((a) => a.command)])],
      required_evidence: ['evidence.json'],
    },
    scope_contract: {
      write_allow: task.write_allow ?? spec.default_write_allow,
      write_deny: task.write_deny ?? spec.default_write_deny,
      out_of_scope: task.out_of_scope ?? [
        'Mudanças fora dos arquivos declarados na write_allow',
        'Alterações destrutivas em dados de produção',
      ],
    },
    budget_contract: {
      max_tokens: 200000,
      max_cost_usd: 4,
      max_duration_minutes: 120,
      max_agent_attempts: 3,
      max_llm_calls: 80,
      max_tool_calls: 200,
      max_shell_executions: 40,
    },
    escalation_contract: {
      on_write_violation: 'block_and_escalate',
      on_budget_exceeded: 'escalate_to_strong_agent',
      on_attempt_failed: 'retry_with_strong',
    },
    _contract_id: `CTR-${String(index + 1).padStart(3, '0')}`,
  };
}

function main(): void {
  const specPath = process.argv[2];
  if (!specPath) throw new Error('uso: bun scripts/pwn-work-gen.ts .piwerness/specs/<work>.json');
  const spec = readSpec(specPath);
  const dir = workDir(spec);
  const now = new Date().toISOString();

  if (spec.legacy_spec && fs.existsSync(path.join(ROOT, spec.legacy_spec))) {
    fs.mkdirSync(dir, { recursive: true });
    fs.copyFileSync(path.join(ROOT, spec.legacy_spec), path.join(dir, 'legacy-spec.md'));
  }

  writeJson(path.join(dir, 'intake.json'), {
    work_id: spec.work_id,
    created_at: now,
    objective: spec.title,
    requester: 'operator',
    status: 'accepted',
  });

  writeJson(path.join(dir, 'discovery.json'), {
    work_id: spec.work_id,
    title: spec.title,
    grilling_depth: 'deep',
    problem: spec.problem,
    actors: spec.actors.map((a) => a.id),
    actor_details: spec.actors,
    objectives: spec.objectives,
    constraints: spec.constraints,
    ...(spec.legacy_spec ? { legacy_spec: 'legacy-spec.md' } : {}),
  });

  writeJson(path.join(dir, 'requirements.json'), {
    work_id: spec.work_id,
    requirements: spec.requirements.map((r) => ({
      id: r.id,
      title: r.title,
      user_story: r.user_story,
      priority: r.priority ?? 'must',
      acceptance_criteria: r.acceptance_criteria,
    })),
  });

  writeJson(path.join(dir, 'prd.json'), {
    $schema: '../../../schemas/prd.schema.json',
    id: `PRD-${spec.work_id}`,
    meta: { version: '1.0', created_at: now, updated_at: now, author: 'operator' },
    title: `PRD — ${spec.title}`.slice(0, 100),
    status: 'approved',
    scenario: spec.scenario,
    problem: { statement: spec.problem },
    solution: { statement: spec.solution },
    decisions: spec.decisions,
    requirements: spec.requirements.map((r) => ({
      id: r.id,
      type: 'functional',
      statement: `${r.title}: ${r.user_story}`,
      priority: r.priority ?? 'must',
    })),
    stakeholders: spec.actors.map((a) => ({ name: a.name, role: a.id })),
    scope: {
      in_scope: spec.objectives,
      out_of_scope: spec.constraints.filter((c) => /fora de escopo/i.test(c)),
    },
    accepted_requirements: spec.requirements.map((r) => r.id),
  });

  writeJson(path.join(dir, 'spec.json'), {
    work_id: spec.work_id,
    title: spec.title,
    capabilities: spec.capabilities,
    rules: spec.rules,
  });

  writeJson(path.join(dir, 'plan.json'), {
    $schema: '../../../schemas/plan.schema.json',
    work_id: spec.work_id,
    title: spec.title,
    components: spec.components,
    global_gates: spec.global_gates,
    tasks: spec.tasks.map((t) => ({
      id: t.id,
      title: t.title,
      requirement_id: t.requirement_id,
      spec_reference: t.spec_reference,
      contract_id: `CTR-${String(spec.tasks.indexOf(t) + 1).padStart(3, '0')}`,
      complexity: t.complexity ?? 'standard',
      depends_on: t.depends_on ?? [],
      behavior: t.behavior,
      components: t.components,
      files: t.files,
      implementation_files: t.implementation_files,
      test_files: t.test_files,
      red: {
        command: t.acceptance_criteria[0]?.command ?? 'go test ./...',
        description: `a asserção do comportamento "${t.behavior}" falha antes da implementação`,
      },
      implementation_steps: t.implementation_steps,
      acceptance_criteria: t.acceptance_criteria,
      visual: t.visual ?? 'N/A',
      documentation: t.documentation ?? 'N/A',
      status: 'pending',
    })),
  });

  spec.tasks.forEach((task, index) => {
    const contract = contractFor(spec, task, index);
    delete contract._contract_id;
    writeJson(path.join(dir, `CTR-${String(index + 1).padStart(3, '0')}.json`), contract);
  });

  writeJson(path.join(dir, 'traceability-matrix.json'), {
    work_id: spec.work_id,
    matrix: spec.tasks.map((t, index) => ({
      origin: `DISC-${spec.work_id}`,
      requirement_id: t.requirement_id,
      decision_id: spec.decisions[0]?.id ?? 'DEC-001',
      spec_section: t.spec_reference,
      task_id: t.id,
      contract_id: `CTR-${String(index + 1).padStart(3, '0')}`,
      evidence_id: 'EVD-001',
      status: 'planned',
    })),
  });

  process.stdout.write(`✓ Work ${spec.work_id} gerado em ${dir} (${spec.tasks.length} tasks)\n`);
}

main();

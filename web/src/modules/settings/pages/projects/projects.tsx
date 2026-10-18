import { useState, type FormEvent } from "react";

import { useIsAdmin } from "@/modules/auth";

import { ApiErrorAlert } from "../../components/api-error-alert";
import { FormField } from "../../components/form-field";
import { ENTITY_ID_HINT, ENTITY_ID_PATTERN, SCOPE_HINT } from "../../constants/settings";
import { useCreateMapping, useCreateProject, useDeleteMapping, useUpdateMapping } from "../../hooks/use-settings-mutations";
import { useMappings, useProjects, useSettingsProviders } from "../../hooks/use-settings-queries";
import type { IProject, IProjectMapping, IProjectMappingUpdate, IProvider } from "../../services/settings-service";

import { projectsStyles as styles } from "./projects.styles";

export function ProjectsPage() {
  const isAdmin = useIsAdmin();
  const projects = useProjects();
  const providers = useSettingsProviders();
  const rows = projects.data ?? [];

  return (
    <section className={styles.section} aria-labelledby="projects-title">
      <header>
        <h2 id="projects-title" className={styles.sectionTitle}>
          Projects
        </h2>
        <p className={styles.sectionHint}>
          The unit of ownership and permission. Map each project to the OpenStack projects or Proxmox pools it owns;
          resources without a mapping show as unassigned.
        </p>
      </header>

      {projects.isError && <ApiErrorAlert error={projects.error} />}
      {projects.isPending ? (
        <p className={styles.muted}>Loading…</p>
      ) : rows.length === 0 ? (
        <p className={styles.empty}>No projects yet.</p>
      ) : (
        <ul className={styles.list}>
          {rows.map((p) => (
            <ProjectCard key={p.id} project={p} providers={providers.data ?? []} isAdmin={isAdmin} />
          ))}
        </ul>
      )}

      {isAdmin && <CreateProjectForm />}
    </section>
  );
}

function ProjectCard({ project, providers, isAdmin }: { project: IProject; providers: IProvider[]; isAdmin: boolean }) {
  const mappings = useMappings(project.id);
  const providerName = new Map(providers.map((p) => [p.id, p.name]));
  const rows = mappings.data ?? [];

  return (
    <li className={styles.card} aria-labelledby={`project-${project.id}`}>
      <div className={styles.projectHead}>
        <span id={`project-${project.id}`} className={styles.projectName}>
          {project.name}
        </span>
        <span className={styles.mono}>{project.id}</span>
      </div>
      {mappings.isError && <ApiErrorAlert error={mappings.error} />}
      {rows.length === 0 && !mappings.isPending ? (
        <p className={styles.muted}>No mappings: this project owns no resources yet.</p>
      ) : (
        <ul className={styles.mappingList} aria-label={`Mappings of ${project.name}`}>
          {rows.map((m) => (
            <MappingRow key={m.id} mapping={m} providers={providers} providerName={providerName} isAdmin={isAdmin} />
          ))}
        </ul>
      )}
      {isAdmin && providers.length > 0 && <AddMappingForm projectId={project.id} providers={providers} />}
    </li>
  );
}

function MappingRow({
  mapping: m,
  providers,
  providerName,
  isAdmin,
}: {
  mapping: IProjectMapping;
  providers: IProvider[];
  providerName: Map<string, string>;
  isAdmin: boolean;
}) {
  const [mode, setMode] = useState<"view" | "edit" | "confirm">("view");
  const [providerId, setProviderId] = useState(m.providerId);
  const [scope, setScope] = useState(m.externalScope);
  const update = useUpdateMapping(m.projectId);
  const remove = useDeleteMapping(m.projectId);
  const label = `${providerName.get(m.providerId) ?? m.providerId} → ${m.externalScope}`;

  if (mode === "edit") {
    const provider = providers.find((p) => p.id === providerId);
    const body: IProjectMappingUpdate = {};
    if (providerId !== m.providerId) body.providerId = providerId;
    if (scope.trim() !== m.externalScope) body.externalScope = scope;
    return (
      <li className={styles.mappingEdit}>
        <form
          className={styles.inlineForm}
          aria-label={`Edit mapping ${label}`}
          onSubmit={(e: FormEvent) => {
            e.preventDefault();
            update.mutate({ id: m.id, body }, { onSuccess: () => setMode("view") });
          }}
        >
          <ApiErrorAlert error={update.error} />
          <FormField label="Provider">
            <select className={styles.input} value={providerId} onChange={(e) => setProviderId(e.target.value)}>
              {providers.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name}
                </option>
              ))}
            </select>
          </FormField>
          <FormField label="External scope" hint={provider ? SCOPE_HINT[provider.type] : undefined}>
            <input className={styles.input} required value={scope} onChange={(e) => setScope(e.target.value)} />
          </FormField>
          <div className={styles.rowActions}>
            <button type="submit" className={styles.secondary} disabled={update.isPending || Object.keys(body).length === 0}>
              Save
            </button>
            <button type="button" className={styles.secondary} onClick={() => setMode("view")}>
              Cancel
            </button>
          </div>
        </form>
      </li>
    );
  }

  return (
    <li className={styles.mapping}>
      <span>{providerName.get(m.providerId) ?? m.providerId}</span>
      <span className={styles.arrow} aria-hidden="true">
        →
      </span>
      <span className={styles.mono}>{m.externalScope}</span>
      {isAdmin && mode === "view" && (
        <span className={styles.rowActions}>
          <button type="button" className={styles.linkButton} onClick={() => setMode("edit")} aria-label={`Edit mapping ${label}`}>
            Edit
          </button>
          <button type="button" className={styles.linkDanger} onClick={() => setMode("confirm")} aria-label={`Remove mapping ${label}`}>
            Remove
          </button>
        </span>
      )}
      {isAdmin && mode === "confirm" && (
        <span className={styles.rowActions}>
          <span className={styles.muted}>Resources it assigned become unassigned on the next sync.</span>
          <button type="button" className={styles.danger} disabled={remove.isPending} onClick={() => remove.mutate(m.id)}>
            Confirm remove
          </button>
          <button type="button" className={styles.secondary} onClick={() => setMode("view")}>
            Cancel
          </button>
        </span>
      )}
      <ApiErrorAlert error={remove.error} />
    </li>
  );
}

function AddMappingForm({ projectId, providers }: { projectId: string; providers: IProvider[] }) {
  const create = useCreateMapping(projectId);
  const [providerId, setProviderId] = useState(providers[0]?.id ?? "");
  const [externalScope, setExternalScope] = useState("");
  const provider = providers.find((p) => p.id === providerId);

  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    create.mutate({ providerId, externalScope }, { onSuccess: () => setExternalScope("") });
  };

  return (
    <form className={styles.inlineForm} onSubmit={onSubmit} aria-label={`Add mapping to ${projectId}`}>
      <ApiErrorAlert error={create.error} />
      <FormField label="Provider">
        <select className={styles.input} value={providerId} onChange={(e) => setProviderId(e.target.value)}>
          {providers.map((p) => (
            <option key={p.id} value={p.id}>
              {p.name}
            </option>
          ))}
        </select>
      </FormField>
      <FormField label="External scope" hint={provider ? SCOPE_HINT[provider.type] : undefined}>
        <input className={styles.input} required value={externalScope} onChange={(e) => setExternalScope(e.target.value)} />
      </FormField>
      <button type="submit" className={styles.secondary} disabled={create.isPending}>
        Add mapping
      </button>
    </form>
  );
}

function CreateProjectForm() {
  const create = useCreateProject();
  const [id, setId] = useState("");
  const [name, setName] = useState("");

  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    create.mutate(
      { id, name },
      {
        onSuccess: () => {
          setId("");
          setName("");
        },
      },
    );
  };

  return (
    <form className={styles.card} onSubmit={onSubmit} aria-labelledby="create-project-title">
      <h3 id="create-project-title" className={styles.sectionTitle}>
        Add a project
      </h3>
      <div className={styles.form}>
        <ApiErrorAlert error={create.error} />
        <FormField label="ID" hint={ENTITY_ID_HINT}>
          <input className={styles.input} required pattern={ENTITY_ID_PATTERN} value={id} onChange={(e) => setId(e.target.value)} />
        </FormField>
        <FormField label="Name">
          <input className={styles.input} required value={name} onChange={(e) => setName(e.target.value)} />
        </FormField>
        <div className={styles.actions}>
          <button type="submit" className={styles.primary} disabled={create.isPending}>
            {create.isPending ? "Adding…" : "Add project"}
          </button>
        </div>
      </div>
    </form>
  );
}

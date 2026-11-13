{{- define "phoedox.name" -}}{{ .Chart.Name }}{{- end -}}

{{- define "phoedox.fullname" -}}
{{- if contains .Chart.Name .Release.Name -}}{{ .Release.Name | trunc 53 | trimSuffix "-" }}
{{- else -}}{{ printf "%s-%s" .Release.Name .Chart.Name | trunc 53 | trimSuffix "-" }}{{- end -}}
{{- end -}}

{{- define "phoedox.labels" -}}
app.kubernetes.io/name: {{ include "phoedox.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version }}
{{- end -}}

{{- define "phoedox.selector" -}}
app.kubernetes.io/name: {{ include "phoedox.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: server
{{- end -}}

{{- define "phoedox.image" -}}
{{- $tag := default .Chart.AppVersion .Values.image.tag -}}
{{- if .Values.image.variant -}}{{ $tag = printf "%s-%s" $tag .Values.image.variant }}{{- end -}}
{{ .Values.image.repository }}:{{ $tag }}
{{- end -}}

{{- define "phoedox.dbCluster" -}}{{ include "phoedox.fullname" . }}-db{{- end -}}

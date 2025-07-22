
#
# Defines stateful set for mongodb
#
{{- define "mcp-registry.mongodb-statefulset" }}
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: db-stateful-set
spec:
  selector:
    matchLabels:
      app: mcp-db
  serviceName: "mcp-db"
  template:
    metadata:
      labels:
        app: mcp-db
    spec:
      affinity: {{ toYaml .Values.db.affinity | nindent 8 }}
      securityContext:
        runAsUser: 999
        runAsGroup: 999
        fsGroup: 999
      containers:
      - name: db-server
        image: "{{ required "Missing required value: db.mongo.image" .Values.db.mongo.image }}:{{ .Values.db.mongo.tag | default .Chart.AppVersion }}"
        ports:
        - containerPort: {{ .Values.db.mongo.port }}
          name: {{ .Values.db.mongo.port_name }}
        volumeMounts:
        - name: data
          mountPath: /data/db
        env:
        {{- toYaml .Values.db.mongo.env | nindent 8 }}
        resources:
          requests:
            cpu: {{ .Values.db.mongo.resources.requests.cpu }}
            memory: {{ .Values.db.mongo.resources.requests.memory }}
          limits:
            cpu: {{ .Values.db.mongo.resources.limits.cpu }}
            memory: {{ .Values.db.mongo.resources.limits.memory }}
        securityContext:
          runAsUser: 999
          runAsGroup: 999
          allowPrivilegeEscalation: false
          readOnlyRootFilesystem: false
          capabilities:
            drop:
              - ALL

  volumeClaimTemplates:
  - metadata:
      name: data
    spec:
      accessModes: [ "ReadWriteOnce" ]
      {{- if .Values.db.storage_class_name }}
      storageClassName: {{ .Values.db.storage_class_name }}
      {{- end }}
      resources:
        requests:
          storage: {{ .Values.db.storage_request_size }}
{{- end }}


#
# Defines service for mongodb
#
{{- define "mcp-registry.mongodb-service"}}
apiVersion: v1
kind: Service
metadata:
  name: mcp-db
  labels:
    app: mcp-db
spec:
  type: ClusterIP
  selector:
    app: mcp-db
  ports:
    - port: {{ .Values.registry.db_port }}
      targetPort: {{ .Values.db.mongo.port }}
      name: {{ .Values.db.mongo.port_name }}
{{- end }}

export function schemaOutputFields(schema: Record<string, any>, output: Record<string, unknown>) {
  return Object.entries(output).flatMap(([key, value]) => {
    const field = schema.properties?.[key]
    if (!field?.title) return []
    const index = field.enum?.indexOf(value) ?? -1
    const display =
      index >= 0 && field.enumLabels?.[index] !== undefined
        ? field.enumLabels[index]
        : typeof value === 'boolean'
          ? value
            ? '是'
            : '否'
          : typeof value === 'object'
            ? JSON.stringify(value)
            : String(value ?? '—')
    return [{ key, title: field.title, value: display }]
  })
}

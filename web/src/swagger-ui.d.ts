declare module 'swagger-ui' {
  const SwaggerUI: (config: {
    domNode: HTMLElement | null
    url: string
    docExpansion?: string
    tryItOutEnabled?: boolean
    supportedSubmitMethods?: string[]
    persistAuth?: boolean
  }) => void
  export default SwaggerUI
}

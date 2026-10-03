import { rpcSchemas, type RPCName, type RPCs } from '#load/contracts';
import { encodeJSON, decodeJSON } from '#load/json';

const responseParsers: { [K in RPCName]: { parse(value: unknown): RPCs[K]['output'] } } = {
  'Auth.Me': rpcSchemas['Auth.Me'].output,
  'AccessRequests.List': rpcSchemas['AccessRequests.List'].output,
  'AccessRequests.ListRequestableConnections': rpcSchemas['AccessRequests.ListRequestableConnections'].output,
  'AccessRequests.Create': rpcSchemas['AccessRequests.Create'].output,
  'AccessRequests.Submit': rpcSchemas['AccessRequests.Submit'].output,
  'AccessRequests.Get': rpcSchemas['AccessRequests.Get'].output,
  'AccessRequests.Approve': rpcSchemas['AccessRequests.Approve'].output,
  'AccessRequests.Cancel': rpcSchemas['AccessRequests.Cancel'].output,
  'QueryExecutions.Execute': rpcSchemas['QueryExecutions.Execute'].output,
  'QueryExecutions.Get': rpcSchemas['QueryExecutions.Get'].output,
  'QueryExecutions.Cancel': rpcSchemas['QueryExecutions.Cancel'].output,
  'QueryExecutions.GetResult': rpcSchemas['QueryExecutions.GetResult'].output,
};

/** Builds the Connect JSON request for a method-specific input. */
export function createRPCRequest<K extends RPCName>(baseURL: string, name: K, body: RPCs[NoInfer<K>]['input']): { url: string; body: string } {
  const [service, method] = name.split('.');
  return { url: `${baseURL.replace(/\/$/, '')}/portcullis.v1.${service}/${method}`, body: encodeJSON(rpcSchemas[name].input, body) };
}

/** Returns only validated method-specific response fields and ProtoJSON defaults. */
export function decodeRPCResponse<K extends RPCName>(name: K, body: string): RPCs[K]['output'] {
  return decodeJSON<RPCs[K]['output']>(responseParsers[name], body);
}

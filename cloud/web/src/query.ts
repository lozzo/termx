import { useQuery, type QueryKey } from '@tanstack/react-query'
import type { DescMessage, MessageShape } from '@bufbuild/protobuf'
import { protoGet } from './api'

export function useProtoQuery<Schema extends DescMessage>(key: QueryKey, path: string, schema: Schema, staleTime = 10_000) {
  return useQuery<MessageShape<Schema>>({ queryKey: key, queryFn: () => protoGet(path, schema), staleTime })
}
